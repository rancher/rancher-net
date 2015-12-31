package backend

type Backend interface {
	Start(logFile string)
	Reload() error
}
