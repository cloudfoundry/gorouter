package routing_api

type Error struct {
	Type    string `json:"name"`
	Message string `json:"message"`
}

func (err Error) Error() string {
	return err.Message
}

const (
	ProcessRequestError  = "ProcessRequestError"
	RouteInvalidError    = "RouteInvalidError"
	DBCommunicationError = "DBCommunicationError"
	UnauthorizedError    = "UnauthorizedError"
)
