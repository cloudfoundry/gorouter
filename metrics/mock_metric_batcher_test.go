package metrics_test

type mockMetricBatcher struct {
	BatchIncrementCounterCalled chan bool
	BatchIncrementCounterInput  struct {
		Name chan string
	}
	BatchIncrementCounterCallCount chan int
	BatchAddCounterCalled          chan bool
	BatchAddCounterInput           struct {
		Name  chan string
		Delta chan uint64
	}
	CloseCalled chan bool
}

func newMockMetricBatcher() *mockMetricBatcher {
	m := &mockMetricBatcher{}
	m.BatchIncrementCounterCalled = make(chan bool, 100)
	m.BatchIncrementCounterCallCount = make(chan int, 100)
	m.BatchIncrementCounterInput.Name = make(chan string, 100)
	m.BatchAddCounterCalled = make(chan bool, 100)
	m.BatchAddCounterInput.Name = make(chan string, 100)
	m.BatchAddCounterInput.Delta = make(chan uint64, 100)
	m.CloseCalled = make(chan bool, 100)
	return m
}
func (m *mockMetricBatcher) BatchIncrementCounter(name string) {
	m.BatchIncrementCounterCalled <- true
	m.BatchIncrementCounterInput.Name <- name
	m.BatchIncrementCounterCallCount <- 1
}
func (m *mockMetricBatcher) BatchAddCounter(name string, delta uint64) {
	m.BatchAddCounterCalled <- true
	m.BatchAddCounterInput.Name <- name
	m.BatchAddCounterInput.Delta <- delta
}
func (m *mockMetricBatcher) Close() {
	m.CloseCalled <- true
}
