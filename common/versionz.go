package common

type Versionz struct {
	VersionRoute string
}

func NewVersionz() *Versionz {
	return &Versionz{VersionRoute: "0"}
} 

func (v *Versionz) Value() string {
	if v.VersionRoute == "" {
		v.VersionRoute = "0"
	}
	return v.VersionRoute
}