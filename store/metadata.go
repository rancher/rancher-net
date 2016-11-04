package store

import (
	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"net"
	"strings"
)

const (
	defaultMetadataURL = "http://rancher-metadata/2015-12-19"
)

// MetadataStore contains information related to metadata client, etc
type MetadataStore struct {
	mc       metadata.Client
	self     Entry
	entries  []Entry
	local    map[string]Entry
	remote   map[string]Entry
	peersMap map[string]Entry
	info     *InfoFromMetadata
}

// InfoFromMetadata stores the information that has been fetched from
// metadata server
type InfoFromMetadata struct {
	selfContainer metadata.Container
	selfHost      metadata.Host
	selfService   metadata.Service
	hosts         []metadata.Host
	containers    []metadata.Container
	hostsMap      map[string]metadata.Host
}

// NewMetadataStoreWithClientIP creates, intializes and returns a store for use with a specific Client IP to contact the metadata
func NewMetadataStoreWithClientIP(userURL, clientIP string) (*MetadataStore, error) {
	var metadataURL string
	if userURL != "" {
		metadataURL = userURL
	} else {
		metadataURL = defaultMetadataURL
	}

	logrus.Debugf("Creating new MetadataStore, metadataURL: %v, clientIP: %v", metadataURL, clientIP)
	mc, err := metadata.NewClientWithIPAndWait(metadataURL, clientIP)
	if err != nil {
		logrus.Errorf("couldn't create metadata client: %v", err)
		return nil, err
	}

	ms := &MetadataStore{}
	ms.mc = mc

	return ms, nil
}

// NewMetadataStore creates, intializes and returns a store for use
func NewMetadataStore(userURL string) (*MetadataStore, error) {
	var metadataURL string
	if userURL != "" {
		metadataURL = userURL
	} else {
		metadataURL = defaultMetadataURL
	}

	logrus.Debugf("Creating new MetadataStore, metadataURL: %v", metadataURL)
	mc, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		logrus.Errorf("couldn't create metadata client: %v", err)
		return nil, err
	}

	ms := &MetadataStore{}
	ms.mc = mc

	return ms, nil
}

// LocalHostIpAddress returns the IP address of the host where the agent is running
func (ms *MetadataStore) LocalHostIpAddress() string {
	return ms.self.HostIpAddress
}

// LocalIpAddress returns the IP address of the current agent
func (ms *MetadataStore) LocalIpAddress() string {
	ip, _, err := net.ParseCIDR(ms.self.IpAddress)
	if err != nil {
		logrus.Errorf("error: %v", err)
		return ""
	}

	return ip.String()
}

// IsRemote is used to check if the given IP addresss is available on the local host or remote
func (ms *MetadataStore) IsRemote(ipAddress string) bool {
	if _, ok := ms.local[ipAddress]; ok {
		logrus.Debugf("Local: %s", ipAddress)
		return false
	}

	_, ok := ms.remote[ipAddress]
	if ok {
		logrus.Debugf("Remote: %s", ipAddress)
	}
	return ok
}

// Entries is used to get all the entries in the database
func (ms *MetadataStore) Entries() []Entry {
	return ms.entries
}

func (ms *MetadataStore) getEntryFromContainer(c metadata.Container) (Entry, error) {
	logrus.Debugf("Getting Entry from Container: %+v", c)

	isSelf := (c.PrimaryIp == ms.info.selfContainer.PrimaryIp)
	isPeer := false

	entry := Entry{
		c.PrimaryIp + "/16",
		ms.info.hostsMap[c.HostUUID].AgentIP,
		isSelf,
		isPeer,
	}

	logrus.Debugf("entry: %+v", entry)
	return entry, nil
}

// RemoteEntriesMap is used to get a map of all entries which are remote
func (ms *MetadataStore) RemoteEntriesMap() map[string]Entry {
	return ms.remote
}

// PeerEntriesMap is used to get a map of entries with only the peers
func (ms *MetadataStore) PeerEntriesMap() map[string]Entry {
	return ms.peersMap
}

// getHostsMapFromHostsArray returns a map of hosts which can be looked up by UUID of the host
func getHostsMapFromHostsArray(hosts []metadata.Host) map[string]metadata.Host {
	hostsMap := map[string]metadata.Host{}

	for _, h := range hosts {
		logrus.Debugf("h: %v", h)
		hostsMap[h.UUID] = h
	}

	logrus.Debugf("hostsMap: %v", hostsMap)
	return hostsMap
}

func (ms *MetadataStore) doInternalRefresh() {
	logrus.Debugf("Doing internal refresh")

	ms.self, _ = ms.getEntryFromContainer(ms.info.selfContainer)

	entries := []Entry{}
	local := map[string]Entry{}
	remote := map[string]Entry{}
	peersMap := map[string]Entry{}

	for _, sc := range ms.info.selfService.Containers {
		e, _ := ms.getEntryFromContainer(sc)
		e.Peer = true
		ipNoCidr := strings.Split(e.IpAddress, "/")[0]
		peersMap[ipNoCidr] = e
	}

	for _, c := range ms.info.containers {
		if c.NetworkUUID != ms.info.selfContainer.NetworkUUID || c.PrimaryIp == "" ||
			c.NetworkFromContainerUUID != "" {
			continue
		}

		e, _ := ms.getEntryFromContainer(c)

		ipNoCidr := strings.Split(e.IpAddress, "/")[0]
		if _, ok := peersMap[ipNoCidr]; ok {
			e.Peer = true
		}

		if e.HostIpAddress == ms.self.HostIpAddress {
			local[ipNoCidr] = e
		} else {
			remote[ipNoCidr] = e
		}

		entries = append(entries, e)
	}

	logrus.Debugf("entries: %v", entries)
	logrus.Debugf("peersMap: %v", peersMap)

	ms.entries = entries
	ms.peersMap = peersMap
	ms.local = local
	ms.remote = remote
}

// Reload is used to refresh/reload the data from metadata
func (ms *MetadataStore) Reload() error {
	logrus.Debugf("Reloading ...")

	selfContainer, err := ms.mc.GetSelfContainer()
	if err != nil {
		logrus.Errorf("couldn't get self container from metadata: %v", err)
		return err
	}

	selfHost, err := ms.mc.GetSelfHost()
	if err != nil {
		logrus.Errorf("couldn't get self host from metadata: %v", err)
		return err
	}

	hosts, err := ms.mc.GetHosts()
	if err != nil {
		logrus.Errorf("couldn't get hosts from metadata: %v", err)
		return err
	}

	containers, err := ms.mc.GetContainers()
	if err != nil {
		logrus.Errorf("couldn't get containers from metadata: %v", err)
		return err
	}

	selfService, err := ms.mc.GetSelfService()
	if err != nil {
		logrus.Errorf("couldn't get self service from metadata: %v", err)
		return err
	}

	hostsMap := getHostsMapFromHostsArray(hosts)

	info := &InfoFromMetadata{
		selfContainer,
		selfHost,
		selfService,
		hosts,
		containers,
		hostsMap,
	}

	ms.info = info

	ms.doInternalRefresh()

	return nil
}
