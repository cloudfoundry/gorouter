package db

type DBError struct {
	Type    string
	Message string
}

func (err DBError) Error() string {
	return err.Message
}

const (
	KeyNotFound       = "KeyNotFound"
	NonUpdatableField = "NonUpdatableField"
	UniqueField       = "UniqueField"
)
