package routing_api

type Error struct {
	Type    string `json:"name"`
	Message string `json:"message"`
}

func (err Error) Error() string {
	return err.Message
}

func NewError(errType string, message string) Error {
	return Error{
		Type:    errType,
		Message: message,
	}
}

const (
	ProcessRequestError         = "ProcessRequestError"
	RouteInvalidError           = "RouteInvalidError"
	RouteServiceUrlInvalidError = "RouteServiceUrlInvalidError"
	DBCommunicationError        = "DBCommunicationError"
	UnauthorizedError           = "UnauthorizedError"
	TcpRouteMappingInvalidError = "TcpRouteMappingInvalidError"
)
