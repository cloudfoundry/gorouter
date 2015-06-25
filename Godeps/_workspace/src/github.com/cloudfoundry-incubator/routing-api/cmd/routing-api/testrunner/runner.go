package testrunner

import (
	"os/exec"
	"strconv"

	"github.com/tedsuo/ifrit/ginkgomon"
)

type Args struct {
	Port         uint16
	ConfigPath   string
	DevMode      bool
	EtcdCluster  string
	IP           string
	SystemDomain string
}

func (args Args) ArgSlice() []string {
	return []string{
		"-port", strconv.Itoa(int(args.Port)),
		"-ip", args.IP,
		"-systemDomain", args.SystemDomain,
		"-config", args.ConfigPath,
		"-devMode=" + strconv.FormatBool(args.DevMode),
		args.EtcdCluster,
	}
}

func New(binPath string, args Args) *ginkgomon.Runner {
	return ginkgomon.New(ginkgomon.Config{
		Name:       "routing-api",
		Command:    exec.Command(binPath, args.ArgSlice()...),
		StartCheck: "started",
	})
}
