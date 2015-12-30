package store

type Entry struct {
	IpAddress     string `json:"ip"`
	HostIpAddress string `json:"hostIp"`
	Self          bool   `json:"self"`
	Peer          bool   `json:"peer"`
}

type Store interface {
	LocalHostIpAddress() string
	LocalIpAddress() string
	IsRemote(ipAddress string) bool
	Entries() []Entry
	Reload() error
}
