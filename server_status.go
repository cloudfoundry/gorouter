package router

import (
	"sync"
	"time"
)

const RPSInterval = 30 // in seconds

type HttpMetrics map[string]*HttpMetric

type ServerStatus struct {
	// NOTE: Due to this golang bug http://golang.org/issue/3069
	//       embedded anonymous fields are ignored by json marshaller,
	//       so all the fields in HttpMetric won't appear in json message.
	//
	//       Good news is the fix of this bug is targetted at go 1.1
	HttpMetric
	sync.Mutex

	Urls           int                    `json:"urls"`
	Droplets       int                    `json:"droplets"`
	BadRequests    int                    `json:"bad_requests"`
	Tags           map[string]HttpMetrics `json:"tags"`
	RequestsPerSec int                    `json:"requests_per_sec"`
	Top10Apps      [10]AppRPS             `json:"top10_app_requests"`
	appRequests    *AppRequests
}

type HttpMetric struct {
	Requests     int           `json:"requests"`
	Latency      *Distribution `json:"latency"`
	Responses2xx int           `json:"responses_2xx"`
	Responses3xx int           `json:"responses_3xx"`
	Responses4xx int           `json:"responses_4xx"`
	Responses5xx int           `json:"responses_5xx"`
	ResponsesXxx int           `json:"responses_xxx"`
}

type AppRPS struct {
	Url string `json:"url"`
	Rps int    `json:"rps"`
}

func NewServerStatus() *ServerStatus {
	s := new(ServerStatus)

	s.Tags = make(map[string]HttpMetrics)
	s.appRequests = NewAppRequests()

	s.Latency = NewDistribution(s, "overall")
	s.Latency.Reset()

	tags := []string{"component", "framework", "runtime"}
	for _, tag := range tags {
		s.Tags[tag] = make(HttpMetrics)
	}

	go func() {
		for {
			time.Sleep(RPSInterval * time.Second)

			s.updateRPS()
		}
	}()

	return s
}

func NewHttpMetric(name string) *HttpMetric {
	m := new(HttpMetric)

	m.Latency = NewDistribution(m, name)
	m.Latency.Reset()

	return m
}

func (s *ServerStatus) updateRPS() {
	requests := s.appRequests.SnapshotAndReset()

	tops := requests.SortedSlice()

	s.Lock()
	defer s.Unlock()

	for i := 0; i < 10; i++ {
		if i >= len(tops) {
			break
		}

		s.Top10Apps[i] = AppRPS{tops[i].Url, tops[i].Num / RPSInterval}
	}

	s.RequestsPerSec = requests.Total / RPSInterval
}

func (s *ServerStatus) RegisterApp(app string) {
	s.appRequests.Register(app)
}

func (s *ServerStatus) UnregisterApp(app string) {
	s.appRequests.Unregister(app)
}

func (s *ServerStatus) IncRequests() {
	s.Lock()
	defer s.Unlock()

	s.Requests++
}

func (s *ServerStatus) IncAppRequests(url string) {
	s.appRequests.Inc(url)
}

func (s *ServerStatus) IncBadRequests() {
	s.Lock()
	defer s.Unlock()

	s.BadRequests++
}

func (s *ServerStatus) IncRequestsWithTags(tags map[string]string) {
	s.Lock()
	defer s.Unlock()

	for key, value := range tags {
		if s.Tags[key] == nil {
			continue
		}

		if s.Tags[key][value] == nil {
			s.Tags[key][value] = NewHttpMetric(key + "." + value)
		}
		s.Tags[key][value].Requests++
	}
}

func (s *ServerStatus) RecordResponse(status int, latency int, tags map[string]string) {
	if latency < 0 {
		return
	}

	s.Lock()
	defer s.Unlock()

	s.record(status, latency)

	for key, value := range tags {
		if s.Tags[key] == nil {
			continue
		}

		if s.Tags[key][value] == nil {
			s.Tags[key][value] = NewHttpMetric(key + "." + value)
		}
		s.Tags[key][value].record(status, latency)
	}
}

func (m *HttpMetric) record(status int, latency int) {
	if status >= 200 && status < 300 {
		m.Responses2xx++
	} else if status >= 300 && status < 400 {
		m.Responses3xx++
	} else if status >= 400 && status < 500 {
		m.Responses4xx++
	} else if status >= 500 && status < 600 {
		m.Responses5xx++
	} else {
		m.ResponsesXxx++
	}

	m.Latency.Add(int64(latency))
}
