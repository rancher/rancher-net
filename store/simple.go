package store

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
)

// Simple ...
type Simple struct {
	sync.Mutex

	file       string
	ipOverride string

	config Config
}

// Config ...
type Config struct {
	hostIP            string
	cidrIP            net.IP
	cidrNetwork       *net.IPNet
	local             map[string]Entry
	remote            map[string]Entry
	entries           []Entry
	peers             map[string]Entry
	remoteNonPeersMap map[string]Entry
}

// Records ...
type Records struct {
	Entries []Entry `json:"entries"`
}

// NewSimpleStore ...
func NewSimpleStore(file, localIP string) *Simple {
	s := &Simple{
		file:       file,
		ipOverride: localIP,
	}
	return s
}

func (s *Simple) readFile() ([]Entry, error) {
	content, err := ioutil.ReadFile(s.file)
	if err != nil {
		return nil, err
	}

	var records Records
	if err := json.Unmarshal(content, &records); err != nil {
		return nil, err
	}

	return records.Entries, nil
}

// Reload ...
func (s *Simple) Reload() error {
	entries, err := s.readFile()
	if err != nil {
		return err
	}

	logrus.Debugf("entries: %v", entries)

	var filteredEntries []Entry
	var self *Entry
	peers := make(map[string]Entry)

	for i, entry := range entries {
		if entry.Self {
			if s.ipOverride != "" {
				entries[i].IPAddress = s.ipOverride
			}
			self = &entry
			break
		}
	}

	if self == nil {
		return fmt.Errorf("Failed to find self entry")
	}

	logrus.Debugf("self: %v", self)

	ip, ipNet, err := net.ParseCIDR(self.IPAddress)
	if err != nil {
		return err
	}

	local := map[string]Entry{}
	remote := map[string]Entry{}
	remoteNonPeersMap := map[string]Entry{}

	for _, entry := range entries {
		if entry.IPAddress == "" {
			continue
		}

		ipNoCidr := strings.Split(entry.IPAddress, "/")[0]

		if entry.HostIPAddress == self.HostIPAddress {
			local[ipNoCidr] = entry
		} else {
			remote[ipNoCidr] = entry
			if !entry.Peer {
				remoteNonPeersMap[ipNoCidr] = entry
			}
		}

		filteredEntries = append(filteredEntries, entry)

		if entry.Peer {
			peers[ipNoCidr] = entry
		}

	}

	if s.ipOverride != "" {
		ip, _, err = net.ParseCIDR(s.ipOverride)
		if err != nil {
			return err
		}
	}

	s.Lock()
	defer s.Unlock()
	s.config = Config{
		hostIP:            self.HostIPAddress,
		cidrIP:            ip,
		cidrNetwork:       ipNet,
		local:             local,
		remote:            remote,
		remoteNonPeersMap: remoteNonPeersMap,
		entries:           filteredEntries,
		peers:             peers,
	}

	logrus.Debugf("config: %+v", s.config)

	return nil
}

func (s *Simple) getConfig() Config {
	s.Lock()
	defer s.Unlock()
	return s.config
}

// PeerEntriesMap ...
func (s *Simple) PeerEntriesMap() map[string]Entry {
	s.Lock()
	defer s.Unlock()
	return s.config.peers
}

// RemoteNonPeerEntriesMap ...
func (s *Simple) RemoteNonPeerEntriesMap() map[string]Entry {
	s.Lock()
	defer s.Unlock()
	return s.config.remoteNonPeersMap
}

// LocalHostIPAddress ...
func (s *Simple) LocalHostIPAddress() string {
	return s.getConfig().hostIP
}

// LocalIPAddress ...
func (s *Simple) LocalIPAddress() string {
	return s.getConfig().cidrIP.String()
}

// Entries ...
func (s *Simple) Entries() []Entry {
	return s.getConfig().entries
}

// RemoteEntriesMap ...
func (s *Simple) RemoteEntriesMap() map[string]Entry {
	return s.getConfig().remote
}

// IsRemote ...
func (s *Simple) IsRemote(ipAddress string) bool {
	config := s.getConfig()

	if _, ok := config.local[ipAddress]; ok {
		logrus.Debugf("Local: %s", ipAddress)
		return false
	}

	_, ok := config.remote[ipAddress]
	if ok {
		logrus.Debugf("Remote: %s", ipAddress)
	}
	return ok
}
