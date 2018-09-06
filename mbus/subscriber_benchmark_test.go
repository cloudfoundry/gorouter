package mbus

import (
	"bytes"
	"testing"
)

func BenchmarkCreateRegistryMessage(b *testing.B) {
	message := new(bytes.Buffer)

	message.WriteString(`{
			"host": "192.168.1.1", "port": 1234, "uris": ["foo50000.example.com"],
			"tags": {}, "app": "12345", "stale_threshold_in_seconds": -1,
			"route_service_url": "", "private_instance_id": "id1",
			"private_instance_index": "0", "isolation_segment": ""
		}`)

	for i := 0; i < b.N; i++ {
		msg, err := createRegistryMessage(message.Bytes())
		if err != nil {
			b.Fatalf("Unable to create registry message: %s", err.Error())
		}

		endpoint, err := msg.makeEndpoint()
		if endpoint.ApplicationId != "12345" {
			b.Fatal("Endpoint not successfully created")
		}
	}
}
