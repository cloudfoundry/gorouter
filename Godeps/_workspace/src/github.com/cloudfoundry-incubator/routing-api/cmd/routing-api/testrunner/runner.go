package testrunner

import (
	"os/exec"
	"strconv"

	"github.com/tedsuo/ifrit/ginkgomon"
)

type Args struct {
	Port        int
	ConfigPath  string
	DevMode     bool
	EtcdCluster string
}

func (args Args) ArgSlice() []string {
	return []string{
		"-port", strconv.Itoa(args.Port),
		"-config", args.ConfigPath,
		"-devMode", strconv.FormatBool(args.DevMode),
		args.EtcdCluster,
	}
}

func New(binPath string, args Args) *ginkgomon.Runner {
	return ginkgomon.New(ginkgomon.Config{
		Name:       "routing-api",
		Command:    exec.Command(binPath, args.ArgSlice()...),
		StartCheck: "starting",
	})
}
