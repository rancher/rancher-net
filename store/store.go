package store

// Entry holds the information for each container
type Entry struct {
	IPAddress     string `json:"ip"`
	HostIPAddress string `json:"hostIp"`
	Self          bool   `json:"self"`
	Peer          bool   `json:"peer"`
}

// Store defines the interface for the data store
type Store interface {
	LocalHostIPAddress() string
	LocalIPAddress() string
	IsRemote(ipAddress string) bool
	Entries() []Entry
	RemoteEntriesMap() map[string]Entry
	RemoteNonPeerEntriesMap() map[string]Entry
	PeerEntriesMap() map[string]Entry
	Reload() error
}
