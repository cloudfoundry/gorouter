package router

import (
	. "launchpad.net/gocheck"
	"log"
	"os"
	"testing"
)

func Test(t *testing.T) {
	file, _ := os.OpenFile("/dev/null", os.O_WRONLY, 0666)
	log.SetOutput(file)

	TestingT(t)
}
