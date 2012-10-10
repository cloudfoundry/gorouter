package common

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	ds := formatDuration(time.Duration(d))
	return []byte(fmt.Sprintf(`"%s"`, ds)), nil
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	str := strings.Trim(string(b), "\"")
	u := strings.Split(str, ":")

	ds := u[0]
	di, err := strconv.ParseInt(ds[:len(ds)-1], 10, 64)
	if err != nil {
		return err
	}

	hs := u[1]
	hi, err := strconv.ParseInt(hs[:len(hs)-1], 10, 64)
	if err != nil {
		return err
	}

	hi += di * 24

	u[1] = fmt.Sprintf("%dh", hi)

	dur, err := time.ParseDuration(strings.Join(u[1:], ""))
	if err != nil {
		return err
	}

	*d = Duration(dur)
	return nil
}
