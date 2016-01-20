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

type Simple struct {
	sync.Mutex

	file       string
	ipOverride string

	config Config
}

type Config struct {
	hostIp      string
	cidrIp      net.IP
	cidrNetwork *net.IPNet
	local       map[string]Entry
	remote      map[string]Entry
	entries     []Entry
}

type Records struct {
	Entries []Entry `json:"entries"`
}

func NewSimpleStore(file, localIp string) *Simple {
	s := &Simple{
		file:       file,
		ipOverride: localIp,
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

func (s *Simple) Reload() error {
	entries, err := s.readFile()
	if err != nil {
		return err
	}

	var filteredEntries []Entry
	var self *Entry

	for i, entry := range entries {
		if entry.Self {
			entries[i].IpAddress = s.ipOverride
			self = &entry
			break
		}
	}

	if self == nil {
		return fmt.Errorf("Failed to find self entry")
	}

	ip, ipNet, err := net.ParseCIDR(self.IpAddress)
	if err != nil {
		return err
	}

	local := map[string]Entry{}
	remote := map[string]Entry{}

	for _, entry := range entries {
		if entry.IpAddress == "" {
			continue
		}

		ipNoCidr := strings.Split(entry.IpAddress, "/")[0]

		if entry.HostIpAddress == self.HostIpAddress {
			local[ipNoCidr] = entry
		} else {
			remote[ipNoCidr] = entry
		}

		filteredEntries = append(filteredEntries, entry)
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
		hostIp:      self.HostIpAddress,
		cidrIp:      ip,
		cidrNetwork: ipNet,
		local:       local,
		remote:      remote,
		entries:     filteredEntries,
	}
	return nil
}

func (s *Simple) getConfig() Config {
	s.Lock()
	defer s.Unlock()
	return s.config
}

func (s *Simple) LocalHostIpAddress() string {
	return s.getConfig().hostIp
}

func (s *Simple) LocalIpAddress() string {
	return s.getConfig().cidrIp.String()
}

func (s *Simple) Entries() []Entry {
	return s.getConfig().entries
}

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
