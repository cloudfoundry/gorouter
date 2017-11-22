package mbus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"code.cloudfoundry.org/gorouter/common"
	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/localip"
	"code.cloudfoundry.org/routing-api/models"

	"github.com/mailru/easyjson"
	"github.com/nats-io/nats"
	"github.com/uber-go/zap"
)

// RegistryMessage defines the format of a route registration/unregistration
// easyjson:json
type RegistryMessage struct {
	Host                    string            `json:"host"`
	Port                    uint16            `json:"port"`
	TLSPort                 uint16            `json:"tls_port"`
	Uris                    []route.Uri       `json:"uris"`
	Tags                    map[string]string `json:"tags"`
	App                     string            `json:"app"`
	StaleThresholdInSeconds int               `json:"stale_threshold_in_seconds"`
	RouteServiceURL         string            `json:"route_service_url"`
	PrivateInstanceID       string            `json:"private_instance_id"`
	ServerCertDomainSAN     string            `json:"server_cert_domain_san"`
	PrivateInstanceIndex    string            `json:"private_instance_index"`
	IsolationSegment        string            `json:"isolation_segment"`
}

func (rm *RegistryMessage) makeEndpoint(acceptTLS bool) (*route.Endpoint, error) {
	port, useTls, err := rm.port(acceptTLS)
	if err != nil {
		return nil, err
	}
	return route.NewEndpoint(
		rm.App,
		rm.Host,
		port,
		rm.ServerCertDomainSAN,
		rm.PrivateInstanceID,
		rm.PrivateInstanceIndex,
		rm.Tags,
		rm.StaleThresholdInSeconds,
		rm.RouteServiceURL,
		models.ModificationTag{},
		rm.IsolationSegment,
		useTls,
	), nil
}

// ValidateMessage checks to ensure the registry message is valid
func (rm *RegistryMessage) ValidateMessage() bool {
	return rm.RouteServiceURL == "" || strings.HasPrefix(rm.RouteServiceURL, "https")
}

// Prefer TLS Port instead of HTTP Port in Registrty Message
func (rm *RegistryMessage) port(acceptTLS bool) (uint16, bool, error) {
	if !acceptTLS && rm.Port == 0 {
		return 0, false, errors.New("Invalid registry message: backend tls is not enabled")
	} else if acceptTLS && rm.TLSPort != 0 {
		return rm.TLSPort, true, nil
	}
	return rm.Port, false, nil
}

// Subscriber subscribes to NATS for all router.* messages and handles them
type Subscriber struct {
	mbusClient    Client
	routeRegistry registry.Registry
	subscription  *nats.Subscription
	reconnected   <-chan Signal

	params    startMessageParams
	acceptTLS bool

	logger logger.Logger
}

type startMessageParams struct {
	id                               string
	minimumRegisterIntervalInSeconds int
	pruneThresholdInSeconds          int
}

// NewSubscriber returns a new Subscriber
func NewSubscriber(
	mbusClient Client,
	routeRegistry registry.Registry,
	c *config.Config,
	reconnected <-chan Signal,
	l logger.Logger,
) *Subscriber {
	guid, err := uuid.GenerateUUID()
	if err != nil {
		l.Fatal("failed-to-generate-uuid", zap.Error(err))
	}

	return &Subscriber{
		mbusClient:    mbusClient,
		routeRegistry: routeRegistry,

		params: startMessageParams{
			id: fmt.Sprintf("%d-%s", c.Index, guid),
			minimumRegisterIntervalInSeconds: int(c.StartResponseDelayInterval.Seconds()),
			pruneThresholdInSeconds:          int(c.DropletStaleThreshold.Seconds()),
		},
		acceptTLS: c.Backends.EnableTLS,

		reconnected: reconnected,

		logger: l,
	}
}

// Run manages the lifecycle of the subscriber process
func (s *Subscriber) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	s.logger.Info("subscriber-starting")
	if s.mbusClient == nil {
		return errors.New("subscriber: nil mbus client")
	}
	err := s.sendStartMessage()
	if err != nil {
		return err
	}
	err = s.subscribeToGreetMessage()
	if err != nil {
		return err
	}
	s.subscription, err = s.subscribeRoutes()
	if err != nil {
		return err
	}

	close(ready)
	s.logger.Info("subscriber-started")

	for {
		select {
		case <-s.reconnected:
			err := s.sendStartMessage()
			if err != nil {
				s.logger.Error("failed-to-send-start-message", zap.Error(err))
			}
		case <-signals:
			s.logger.Info("exited")
			return nil
		}
	}
}

func (s *Subscriber) Pending() (int, error) {
	if s.subscription == nil {
		s.logger.Error("failed-to-get-subscription")
		return -1, errors.New("NATS subscription is nil, Subscriber must be invoked")
	}

	msgs, _, err := s.subscription.Pending()
	return msgs, err
}

func (s *Subscriber) subscribeToGreetMessage() error {
	_, err := s.mbusClient.Subscribe("router.greet", func(msg *nats.Msg) {
		response, _ := s.startMessage()
		_ = s.mbusClient.Publish(msg.Reply, response)
	})

	return err
}

func (s *Subscriber) subscribeRoutes() (*nats.Subscription, error) {
	natsSubscription, err := s.mbusClient.Subscribe("router.*", func(message *nats.Msg) {
		msg, regErr := createRegistryMessage(message.Data)
		if regErr != nil {
			s.logger.Error("validation-error",
				zap.Error(regErr),
				zap.String("payload", string(message.Data)),
				zap.String("subject", message.Subject),
			)
			return
		}
		switch message.Subject {
		case "router.register":
			s.registerEndpoint(msg)
		case "router.unregister":
			s.unregisterEndpoint(msg)
			s.logger.Info("unregister-route", zap.String("message", string(message.Data)))
		default:
		}
	})

	// Pending limits are set to twice the defaults
	natsSubscription.SetPendingLimits(131072, 131072*1024)

	return natsSubscription, err
}

func (s *Subscriber) registerEndpoint(msg *RegistryMessage) {
	endpoint, err := msg.makeEndpoint(s.acceptTLS)
	if err != nil {
		s.logger.Error("Unable to register route",
			zap.Error(err),
			zap.Object("message", msg),
		)
		return
	}

	for _, uri := range msg.Uris {
		s.routeRegistry.Register(uri, endpoint)
	}
}

func (s *Subscriber) unregisterEndpoint(msg *RegistryMessage) {
	endpoint, err := msg.makeEndpoint(s.acceptTLS)
	if err != nil {
		s.logger.Error("Unable to unregister route",
			zap.Error(err),
			zap.Object("message", msg),
		)
		return
	}
	for _, uri := range msg.Uris {
		s.routeRegistry.Unregister(uri, endpoint)
	}
}

func (s *Subscriber) startMessage() ([]byte, error) {
	host, err := localip.LocalIP()
	if err != nil {
		return nil, err
	}

	d := common.RouterStart{
		Id:    s.params.id,
		Hosts: []string{host},
		MinimumRegisterIntervalInSeconds: s.params.minimumRegisterIntervalInSeconds,
		PruneThresholdInSeconds:          s.params.pruneThresholdInSeconds,
	}
	message, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (s *Subscriber) sendStartMessage() error {
	message, err := s.startMessage()
	if err != nil {
		return err
	}
	// Send start message once at start
	return s.mbusClient.Publish("router.start", message)
}

func createRegistryMessage(data []byte) (*RegistryMessage, error) {
	var msg RegistryMessage

	jsonErr := easyjson.Unmarshal(data, &msg)
	if jsonErr != nil {
		return nil, jsonErr
	}

	if !msg.ValidateMessage() {
		return nil, errors.New("Unable to validate message. route_service_url must be https")
	}

	return &msg, nil
}
