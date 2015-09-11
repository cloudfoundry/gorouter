package route

import (
	"errors"
	"strings"
)

type Uri string

func (u Uri) ToLower() Uri {
	return Uri(strings.ToLower(u.String()))
}

func (u Uri) NextWildcard() (Uri, error) {
	uri := strings.TrimPrefix(u.String(), "*.")

	i := strings.Index(uri, ".")
	if i == -1 {
		return u, errors.New("no next wildcard available")
	}
	suffix := uri[i+1:]
	return Uri("*." + suffix), nil
}

func (u Uri) String() string {
	return strings.TrimSuffix(string(u), "/")
}

func (u Uri) RouteKey() Uri {
	key := u.ToLower()
	if idx := strings.Index(string(key), "?"); idx >= 0 {
		key = key[0:idx]
	}
	return key
}
