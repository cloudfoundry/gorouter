package testhelpers

import "github.com/cloudfoundry/loggregatorlib/cfcomponent/instrumentation"
import . "github.com/onsi/gomega"

func MetricValue(instrumentable instrumentation.Instrumentable, name string) interface{} {
	for _, metric := range instrumentable.Emit().Metrics {
		if metric.Name == name {
			return metric.Value
		}
	}
	return nil
}

func EventuallyExpectMetric(instrumentable instrumentation.Instrumentable, name string, value uint64) {
	Eventually(func() interface{} {
		return MetricValue(instrumentable, name)
	}).Should(Equal(value))
}
