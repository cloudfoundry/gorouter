package models

import "github.com/nu7hatch/gouuid"

type Route struct {
	Route           string          `json:"route"`
	Port            uint16          `json:"port"`
	IP              string          `json:"ip"`
	TTL             int             `json:"ttl"`
	LogGuid         string          `json:"log_guid"`
	RouteServiceUrl string          `json:"route_service_url,omitempty"`
	ModificationTag ModificationTag `json:"modification_tag"`
}

func NewModificationTag() (ModificationTag, error) {
	uuid, err := uuid.NewV4()
	if err != nil {
		return ModificationTag{}, err
	}

	return ModificationTag{
		Guid:  uuid.String(),
		Index: 0,
	}, nil
}

func (t *ModificationTag) Increment() {
	t.Index++
}

func (m *ModificationTag) SucceededBy(other *ModificationTag) bool {
	if m == nil || m.Guid == "" || other.Guid == "" {
		return true
	}

	return m.Guid != other.Guid || m.Index < other.Index
}

func (r Route) Matches(other Route) bool {
	return r.Route == other.Route && r.Port == other.Port && r.IP == other.IP &&
		r.TTL == other.TTL && r.LogGuid == other.LogGuid && r.RouteServiceUrl == other.RouteServiceUrl
}

type ModificationTag struct {
	Guid  string `json:"guid"`
	Index uint32 `json:"index"`
}
