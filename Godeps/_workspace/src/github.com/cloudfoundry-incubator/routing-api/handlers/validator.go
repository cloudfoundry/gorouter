package handlers

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	routing_api "github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/db"
)

//go:generate counterfeiter -o fakes/fake_validator.go . RouteValidator
type RouteValidator interface {
	ValidateCreate(routes []db.Route, maxTTL int) *routing_api.Error
	ValidateDelete(routes []db.Route) *routing_api.Error

	ValidateTcpRouteMapping(tcpRouteMappings []db.TcpRouteMapping) *routing_api.Error
}

type Validator struct{}

func NewValidator() Validator {
	return Validator{}
}

func (v Validator) ValidateCreate(routes []db.Route, maxTTL int) *routing_api.Error {
	for _, route := range routes {
		err := requiredValidation(route)
		if err != nil {
			return err
		}

		if route.TTL > maxTTL {
			err := routing_api.NewError(routing_api.RouteInvalidError, fmt.Sprintf("Max ttl is %d", maxTTL))
			return &err
		}

		if route.TTL <= 0 {
			err := routing_api.NewError(routing_api.RouteInvalidError, "Request requires a ttl greater than 0")
			return &err
		}
	}
	return nil
}

func (v Validator) ValidateDelete(routes []db.Route) *routing_api.Error {
	for _, route := range routes {
		err := requiredValidation(route)
		if err != nil {
			return err
		}
	}
	return nil
}

func requiredValidation(route db.Route) *routing_api.Error {
	err := validateRouteUrl(route.Route)
	if err != nil {
		return err
	}

	err = validateRouteServiceUrl(route.RouteServiceUrl)
	if err != nil {
		return err
	}

	if route.Port <= 0 {
		err := routing_api.NewError(routing_api.RouteInvalidError, "Each route request requires a port greater than 0")
		return &err
	}

	if route.Route == "" {
		err := routing_api.NewError(routing_api.RouteInvalidError, "Each route request requires a valid route")
		return &err
	}

	if route.IP == "" {
		err := routing_api.NewError(routing_api.RouteInvalidError, "Each route request requires an IP")
		return &err
	}

	return nil
}

func validateRouteUrl(route string) *routing_api.Error {
	err := validateUrl(route)
	if err != nil {
		err := routing_api.NewError(routing_api.RouteInvalidError, err.Error())
		return &err
	}

	return nil
}

func validateRouteServiceUrl(routeService string) *routing_api.Error {
	if routeService == "" {
		return nil
	}

	if !strings.HasPrefix(routeService, "https://") {
		err := routing_api.NewError(routing_api.RouteServiceUrlInvalidError, "Route service url must use HTTPS.")
		return &err
	}

	err := validateUrl(routeService)
	if err != nil {
		err := routing_api.NewError(routing_api.RouteServiceUrlInvalidError, err.Error())
		return &err
	}

	return nil
}

func validateUrl(urlToValidate string) error {
	if strings.ContainsAny(urlToValidate, "?#") {
		return errors.New("Url cannot contain any of [?, #]")
	}

	parsedURL, err := url.Parse(urlToValidate)

	if err != nil {
		return err
	}

	if parsedURL.String() != urlToValidate {
		return errors.New("Url cannot contain invalid characters")
	}

	return nil
}

func (v Validator) ValidateTcpRouteMapping(tcpRouteMappings []db.TcpRouteMapping) *routing_api.Error {
	for _, tcpRouteMapping := range tcpRouteMappings {
		err := validateTcpRouteMapping(tcpRouteMapping)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateTcpRouteMapping(tcpRouteMappings db.TcpRouteMapping) *routing_api.Error {

	if tcpRouteMappings.TcpRoute.RouterGroupGuid == "" {
		err := routing_api.NewError(routing_api.TcpRouteMappingInvalidError, "Each tcp mapping requires a valid router group guid")
		return &err
	}

	if tcpRouteMappings.TcpRoute.ExternalPort <= 0 {
		err := routing_api.NewError(routing_api.TcpRouteMappingInvalidError, "Each tcp mapping requires a positive external port")
		return &err
	}

	if tcpRouteMappings.HostIP == "" {
		err := routing_api.NewError(routing_api.TcpRouteMappingInvalidError, "Each tcp mapping requires a non empty host ip")
		return &err
	}

	if tcpRouteMappings.HostPort <= 0 {
		err := routing_api.NewError(routing_api.TcpRouteMappingInvalidError, "Each tcp mapping requires a positive host port")
		return &err
	}

	return nil
}
