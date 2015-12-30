package backend

type Backend interface {
	Start()
	Reload() error
}
