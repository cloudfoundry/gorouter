package route

import (
	"strings"
)

type Uri string

func (u Uri) ToLower() Uri {
	return Uri(strings.ToLower(string(u)))
}

type Uris []Uri

func (ms Uris) Sub(ns Uris) Uris {
	var rs Uris

	for _, m := range ms {
		found := false
		for _, n := range ns {
			if m == n {
				found = true
				break
			}
		}

		if !found {
			rs = append(rs, m)
		}
	}

	return rs
}

func (x Uris) Has(y Uri) bool {
	for _, xb := range x {
		if xb == y {
			return true
		}
	}

	return false
}

func (x Uris) Remove(y Uri) (Uris, bool) {
	for i, xb := range x {
		if xb == y {
			x[i] = x[len(x)-1]
			x = x[:len(x)-1]
			return x, true
		}
	}

	return x, false
}
