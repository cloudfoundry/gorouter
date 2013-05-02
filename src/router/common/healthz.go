package common

import (
  "encoding/json"
)

type Lockable interface {
  Lock()
  Unlock()
}

type Healthz struct {
  LockableObject Lockable
}

func (v *Healthz) MarshalJSON() ([]byte, error) {
  health := make(map[string]string)
  health["health"] = "ok"
	return json.Marshal(health)
}
