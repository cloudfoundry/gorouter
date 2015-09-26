package access_log

//go:generate counterfeiter -o fakes/fake_access_logger.go . AccessLogger
type AccessLogger interface {
	Run()
	Stop()
	Log(record AccessLogRecord)
}
