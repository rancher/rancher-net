package backend

type Backend interface {
	Start(launch bool, logFile string)
	Reload() error
}
