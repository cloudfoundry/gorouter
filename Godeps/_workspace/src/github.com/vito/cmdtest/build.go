package cmdtest

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

func Build(mainPath string, args ...string) (string, error) {
	return BuildIn(os.Getenv("GOPATH"), mainPath, args...)
}

func BuildIn(gopath string, mainPath string, args ...string) (string, error) {
	if len(gopath) == 0 {
		panic("$GOPATH not provided when building " + mainPath)
	}

	tmpdir, err := ioutil.TempDir("", "test_cmd_main")
	if err != nil {
		return "", err
	}

	executable := filepath.Join(tmpdir, filepath.Base(mainPath))

	cmdArgs := append([]string{"build"}, args...)
	cmdArgs = append(cmdArgs, "-o", executable, mainPath)

	build := exec.Command("go", cmdArgs...)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	build.Stdin = os.Stdin
	build.Env = append([]string{"GOPATH=" + gopath}, os.Environ()...)

	err = build.Run()
	if err != nil {
		return "", err
	}

	return executable, nil
}
