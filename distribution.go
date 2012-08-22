package router

import (
	"encoding/json"
	"metrics"
	"time"
)

// Distribution is simply a wrapper of metrics.Distribution.
// It supports marshalling distribution in json format, without
// snapshotting.
type Distribution struct {
	m *metrics.Distribution
	json.Marshaler
}

func NewDistribution(tyep interface{}, name string) *Distribution {
	d := new(Distribution)

	dist := metrics.DefaultRegistry.Find(tyep, name)
	if dist == nil {
		d.m = metrics.NewDistribution(tyep, name)
	} else {
		d.m = dist.(*metrics.Distribution)
	}

	return d
}

// The default rolling window is 10 minutes
func (d *Distribution) SetWindow(nsec time.Duration) {
	d.m.SetWindow(nsec)
}

// The default number of elements is 1000
func (d *Distribution) SetMaxSampleSize(n uint64) {
	d.m.SetMaxSampleSize(n)
}

func (d *Distribution) Add(value int64) {
	d.m.Add(value)
}

func (d *Distribution) Reset() {
	d.m.Reset()
}

func (d *Distribution) Snapshot() metrics.DistributionSnapshot {
	return d.m.Snapshot()
}

func (d *Distribution) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.m.Snapshot())
}
