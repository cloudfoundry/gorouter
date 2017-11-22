package fakes

import (
	"sync"

	"github.com/cloudfoundry/dropsonde/metric_sender"
)

type ValueChainer interface {
	SetTag(key, value string) metric_sender.ValueChainer
	Send() error
}

type FakeValueChainer struct {
	SetTagStub        func(key, value string) metric_sender.ValueChainer
	setTagMutex       sync.RWMutex
	setTagArgsForCall []struct {
		key   string
		value string
	}
	setTagReturns struct {
		result1 metric_sender.ValueChainer
	}
	SendStub        func() error
	sendMutex       sync.RWMutex
	sendArgsForCall []struct{}
	sendReturns     struct {
		result1 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeValueChainer) SetTag(key string, value string) metric_sender.ValueChainer {
	fake.setTagMutex.Lock()
	fake.setTagArgsForCall = append(fake.setTagArgsForCall, struct {
		key   string
		value string
	}{key, value})
	fake.recordInvocation("SetTag", []interface{}{key, value})
	fake.setTagMutex.Unlock()
	if fake.SetTagStub != nil {
		return fake.SetTagStub(key, value)
	}
	return fake.setTagReturns.result1
}

func (fake *FakeValueChainer) SetTagCallCount() int {
	fake.setTagMutex.RLock()
	defer fake.setTagMutex.RUnlock()
	return len(fake.setTagArgsForCall)
}

func (fake *FakeValueChainer) SetTagArgsForCall(i int) (string, string) {
	fake.setTagMutex.RLock()
	defer fake.setTagMutex.RUnlock()
	return fake.setTagArgsForCall[i].key, fake.setTagArgsForCall[i].value
}

func (fake *FakeValueChainer) SetTagReturns(result1 metric_sender.ValueChainer) {
	fake.SetTagStub = nil
	fake.setTagReturns = struct {
		result1 metric_sender.ValueChainer
	}{result1}
}

func (fake *FakeValueChainer) Send() error {
	fake.sendMutex.Lock()
	fake.sendArgsForCall = append(fake.sendArgsForCall, struct{}{})
	fake.recordInvocation("Send", []interface{}{})
	fake.sendMutex.Unlock()
	if fake.SendStub != nil {
		return fake.SendStub()
	}
	return fake.sendReturns.result1
}

func (fake *FakeValueChainer) SendCallCount() int {
	fake.sendMutex.RLock()
	defer fake.sendMutex.RUnlock()
	return len(fake.sendArgsForCall)
}

func (fake *FakeValueChainer) SendReturns(result1 error) {
	fake.SendStub = nil
	fake.sendReturns = struct {
		result1 error
	}{result1}
}

func (fake *FakeValueChainer) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.setTagMutex.RLock()
	defer fake.setTagMutex.RUnlock()
	fake.sendMutex.RLock()
	defer fake.sendMutex.RUnlock()
	return fake.invocations
}

func (fake *FakeValueChainer) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ ValueChainer = new(FakeValueChainer)
