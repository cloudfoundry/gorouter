package common

import (
	"strings"
	"time"
)

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte("\"" + time.Duration(d).String() + "\""), nil
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	str := strings.Trim(string(b), "\"")
	dur, err := time.ParseDuration(str)
	if err != nil {
		return err
	}

	*d = Duration(dur)
	return nil
}
