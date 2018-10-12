package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	logLine := os.Args[1:]

	parsedLine := Parse(logLine[0])
	fmt.Print(parsedLine)
}

func Parse(logLine string) string {
	msg := convertToMessage(logLine)
	out, _ := json.MarshalIndent(msg, "", " ")

	return string(out)
}

type message struct {
	RequestHost      string
	StartDate        string
	RequestMethod    string
	RequestURL       string
	RequestProtocol  string
	StatusCode       string
	BytesReceived    string
	BytesSent        string
	Referer          string
	UserAgent        string
	RemoteAddress    string
	BackendAddress   string
	XForwardedFor    string
	XForwardedProto  string
	VcapRequestID    string
	ResponseTime     string
	ApplicationID    string
	ApplicationIndex string
	ExtraHeaders     string
}

func convertToMessage(logLine string) message {
	cleanedLine := strings.Replace(logLine, "\"", "", -1)
	items := strings.Split(cleanedLine, " ")
	appIndex := getValue(cleanedLine, "app_index:", "")
	msg := message{
		RequestHost:      items[0],
		StartDate:        items[2],
		RequestMethod:    items[3],
		RequestURL:       items[4],
		RequestProtocol:  items[5],
		StatusCode:       items[6],
		BytesReceived:    items[7],
		BytesSent:        items[8],
		Referer:          items[9],
		UserAgent:        items[10],
		RemoteAddress:    items[11],
		BackendAddress:   items[12],
		XForwardedFor:    getValue(cleanedLine, "x_forwarded_for:", " x_forwarded_proto"),
		XForwardedProto:  getValue(cleanedLine, "x_forwarded_proto:", " vcap_request_id"),
		VcapRequestID:    getValue(cleanedLine, "vcap_request_id:", " response_time"),
		ResponseTime:     getValue(cleanedLine, "response_time:", " app_id"),
		ApplicationID:    getValue(cleanedLine, "app_id:", " app_index"),
		ApplicationIndex: appIndex,
		ExtraHeaders:     getValue(cleanedLine, fmt.Sprintf("app_index:%s ", appIndex), "ExtraHeaders"),
	}
	return msg
}

func getValue(line, key string, nextKey string) string {
	keyVals := strings.Split(line, key)
	if nextKey == "ExtraHeaders" {
		return keyVals[1]
	}
	val := strings.Split(keyVals[1], nextKey)
	return val[0]
}
