package runner_support

import (
	"io"

	"github.com/onsi/ginkgo"
)

func TeeToGinkgoWriter(out io.Writer) io.Writer {
	return io.MultiWriter(out, ginkgo.GinkgoWriter)
}
