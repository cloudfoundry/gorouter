package route_test

import (
	"code.cloudfoundry.org/gorouter/route"
	"sync"
	"testing"
	"time"
)

var (
	endpoint1 = route.Endpoint{
		ApplicationId:        "abc",
		Protocol:             "http2",
		Tags:                 map[string]string{"tag1": "value1", "tag2": "value2"},
		ServerCertDomainSAN:  "host.domain.tld",
		PrivateInstanceId:    "1234-5678-91011-0000",
		PrivateInstanceIndex: "",
		Stats:                nil,
		IsolationSegment:     "",
		UpdatedAt:            time.Time{},
		RoundTripperInit:     sync.Once{},
	}
	endpoint2 = route.Endpoint{
		ApplicationId:        "def",
		Protocol:             "http2",
		Tags:                 map[string]string{"tag1": "value1", "tag2": "value2"},
		ServerCertDomainSAN:  "host.domain.tld",
		PrivateInstanceId:    "1234-5678-91011-0000",
		PrivateInstanceIndex: "",
		Stats:                nil,
		IsolationSegment:     "",
		UpdatedAt:            time.Time{},
		RoundTripperInit:     sync.Once{},
	}
	endpoint3 = route.Endpoint{
		ApplicationId:        "abc",
		Protocol:             "http2",
		Tags:                 map[string]string{"tag1": "value1", "tag2": "value2"},
		ServerCertDomainSAN:  "host.domain.tld",
		PrivateInstanceId:    "1234-5678-91011-0000",
		PrivateInstanceIndex: "",
		Stats:                nil,
		IsolationSegment:     "",
		UpdatedAt:            time.Time{},
		RoundTripperInit:     sync.Once{},
	}
	result = false
)

func BenchmarkEndpointEquals(b *testing.B) {
	for i := 0; i < b.N; i++ {
		result = endpoint1.Equal(&endpoint3)
	}
	b.ReportAllocs()
}

func BenchmarkEndpointNotEquals(b *testing.B) {
	for i := 0; i < b.N; i++ {
		result = endpoint1.Equal(&endpoint2)
	}
	b.ReportAllocs()
}
