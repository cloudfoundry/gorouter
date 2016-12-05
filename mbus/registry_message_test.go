package mbus_test

import (
	"encoding/json"

	. "code.cloudfoundry.org/gorouter/mbus"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RegistryMessage", func() {
	Describe("ValidateMessage", func() {
		var message *RegistryMessage
		var payload []byte

		JustBeforeEach(func() {
			message = new(RegistryMessage)
			err := json.Unmarshal(payload, message)
			Expect(err).NotTo(HaveOccurred())
		})

		Describe("With a payload with no route service url", func() {
			BeforeEach(func() {
				payload = []byte(`{"dea":"dea1","app":"app1","uris":["test.com"],"host":"1.2.3.4","port":1234,"tags":{},"private_instance_id":"private_instance_id"}`)
			})

			It("passes validation", func() {
				Expect(message.ValidateMessage()).To(BeTrue())
			})
		})

		Describe("With a payload with an empty route service url", func() {
			BeforeEach(func() {
				payload = []byte(`{"dea":"dea1","app":"app1","uris":["test.com"],"host":"1.2.3.4","port":1234,"tags":{},"route_service_url":"","private_instance_id":"private_instance_id"}`)
			})

			It("passes validation", func() {
				Expect(message.ValidateMessage()).To(BeTrue())
			})
		})

		Describe("With a payload with an https route service url", func() {
			BeforeEach(func() {
				payload = []byte(`{"dea":"dea1","app":"app1","uris":["test.com"],"host":"1.2.3.4","port":1234,"tags":{},"route_service_url":"https://www.my-route.me","private_instance_id":"private_instance_id"}`)
			})

			It("passes validation", func() {
				Expect(message.ValidateMessage()).To(BeTrue())
			})
		})

		Describe("With a payload with an http route service url", func() {
			BeforeEach(func() {
				payload = []byte(`{"dea":"dea1","app":"app1","uris":["test.com"],"host":"1.2.3.4","port":1234,"tags":{},"route_service_url":"http://www.my-insecure-route.com","private_instance_id":"private_instance_id"}`)
			})

			It("fails validation", func() {
				Expect(message.ValidateMessage()).To(BeFalse())
			})
		})
	})
})
