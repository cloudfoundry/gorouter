package fake

import "sync"

type FakeMetricSender struct {
	counters map[string]uint64
	values   map[string]Metric
	sync.RWMutex
}

type Metric struct {
	Value float64
	Unit  string
}

func NewFakeMetricSender() *FakeMetricSender {
	return &FakeMetricSender{
		counters: make(map[string]uint64),
		values:   make(map[string]Metric),
	}
}

func (fms *FakeMetricSender) SendValue(name string, value float64, unit string) error {
	fms.Lock()
	defer fms.Unlock()
	fms.values[name] = Metric{Value: value, Unit: unit}

	return nil
}

func (fms *FakeMetricSender) IncrementCounter(name string) error {
	fms.Lock()
	defer fms.Unlock()
	fms.counters[name]++

	return nil
}

func (fms *FakeMetricSender) AddToCounter(name string, delta uint64) error {
	fms.Lock()
	defer fms.Unlock()
	fms.counters[name] = fms.counters[name] + delta

	return nil
}

func (fms *FakeMetricSender) GetValue(name string) Metric {
	fms.RLock()
	defer fms.RUnlock()

	return fms.values[name]
}

func (fms *FakeMetricSender) GetCounter(name string) uint64 {
	fms.RLock()
	defer fms.RUnlock()

	return fms.counters[name]
}
