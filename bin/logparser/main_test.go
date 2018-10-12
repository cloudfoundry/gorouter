package main_test

import (
	logparser "code.cloudfoundry.org/gorouter/bin/logparser"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parse", func() {
	It("returns parsed lines", func() {
		line := `1234:example.com - [2018-09-25T17:52:08.232+0000] "POST /api/v1/product HTTP/1.1" 502 2272 67 "-" "Java/1.8.0_131" "10.235.62.197:49358" "10.235.56.114:61002" x_forwarded_for:"10.235.56.94, 10.235.62.197" x_forwarded_proto:"https" vcap_request_id:"f4792440-ae87-4f57-40f4-7d1a4394b020" response_time:5.0425708 app_id:"65e29887-73a2-4ecc-9795-70f2ec27b2d2" app_index:"1" x_b3_traceid:"36b156fdb36034d1" x_b3_spanid:"36b156fdb36034d1" x_b3_parentspanid:"-"'`

		parsedLine := logparser.Parse(line)
		Expect(parsedLine).To(Equal(`{
 "RequestHost": "1234:example.com",
 "StartDate": "[2018-09-25T17:52:08.232+0000]",
 "RequestMethod": "POST",
 "RequestURL": "/api/v1/product",
 "RequestProtocol": "HTTP/1.1",
 "StatusCode": "502",
 "BytesReceived": "2272",
 "BytesSent": "67",
 "Referer": "-",
 "UserAgent": "Java/1.8.0_131",
 "RemoteAddress": "10.235.62.197:49358",
 "BackendAddress": "10.235.56.114:61002",
 "XForwardedFor": "10.235.56.94, 10.235.62.197",
 "XForwardedProto": "https",
 "VcapRequestID": "f4792440-ae87-4f57-40f4-7d1a4394b020",
 "ResponseTime": "5.0425708",
 "ApplicationID": "65e29887-73a2-4ecc-9795-70f2ec27b2d2",
 "ApplicationIndex": "1",
 "ExtraHeaders": "x_b3_traceid:36b156fdb36034d1 x_b3_spanid:36b156fdb36034d1 x_b3_parentspanid:-'"
}`))
	})
})
