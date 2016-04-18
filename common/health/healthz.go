package health

type Healthz struct {
}

func (v *Healthz) Value() string {
	return "ok"
}
