package router

import (
	"router/stats"
	"sync"
	"time"
)

const RPSInterval = 30 // in seconds

type HttpMetrics map[string]*HttpMetric

type Varz struct {
	sync.Mutex `json:"-"`

	*Registry `json:"-"`

	// NOTE: Due to this golang bug http://golang.org/issue/3069
	//       embedded anonymous fields are ignored by json marshaller,
	//       so all the fields in HttpMetric won't appear in json message.
	//
	//       Good news is the fix of this bug is targetted at go 1.1
	HttpMetric `encode:"yes"`

	Urls           int                    `json:"urls"`
	Droplets       int                    `json:"droplets"`
	BadRequests    int                    `json:"bad_requests"`
	Tags           map[string]HttpMetrics `json:"tags"`
	RequestsPerSec int                    `json:"requests_per_sec"`
	Top10Apps      [10]AppRPS             `json:"top10_app_requests"`
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
	Rps int64  `json:"rps"`
}

func NewVarz() *Varz {
	s := new(Varz)

	s.Tags = make(map[string]HttpMetrics)

	s.Latency = NewDistribution(s, "overall")
	s.Latency.Reset()

	tags := []string{"component", "framework", "runtime"}
	for _, tag := range tags {
		s.Tags[tag] = make(HttpMetrics)
	}

	go func() {
		rps := time.NewTicker(RPSInterval * time.Second)
		counts := time.NewTicker(1 * time.Second)

		for {
			select {
			case <-rps.C:
				s.updateRPS()
			case <-counts.C:
				s.updateCounts()
			}
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

func (s *Varz) updateCounts() {
	s.Lock()
	defer s.Unlock()

	s.Urls = s.Registry.NumUris()
	s.Droplets = s.Registry.NumBackends()
}

func (s *Varz) updateRPS() {
	t := time.Now().Add(-RPSInterval * time.Second)

	s.Lock()
	defer s.Unlock()

	x := s.Registry.TopApps.TopSince(t, 10)
	for i, y := range x {
		s.Top10Apps[i] = AppRPS{y.ApplicationId, y.Requests / int64(stats.TopAppsEntryLifetime.Seconds())}
	}

	// TODO: use a proper meter for this stat
	s.RequestsPerSec = 0
}

func (s *Varz) IncRequests() {
	s.Lock()
	defer s.Unlock()

	s.Requests++
}

func (s *Varz) IncBadRequests() {
	s.Lock()
	defer s.Unlock()

	s.BadRequests++
}

func (s *Varz) IncRequestsWithTags(tags map[string]string) {
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

func (s *Varz) RecordResponse(status int, latency int, tags map[string]string) {
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
