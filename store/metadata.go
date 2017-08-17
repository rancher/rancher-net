package store

import (
	"fmt"
	"net"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
)

const (
	metadataURLTemplate = "http://%v/2015-12-19"
	defaultSubnetPrefix = "/16"

	// DefaultMetadataAddress specifies the default value to use if nothing is specified
	DefaultMetadataAddress = "169.254.169.250"
)

// MetadataStore contains information related to metadata client, etc
type MetadataStore struct {
	mc                metadata.Client
	self              Entry
	entries           []Entry
	local             map[string]Entry
	remote            map[string]Entry
	peersMap          map[string]Entry
	remoteNonPeersMap map[string]Entry
	info              *InfoFromMetadata
}

// InfoFromMetadata stores the information that has been fetched from
// metadata server
type InfoFromMetadata struct {
	selfContainer           metadata.Container
	selfHost                metadata.Host
	selfService             metadata.Service
	selfNetwork             metadata.Network
	selfNetworkSubnetPrefix string
	services                []metadata.Service
	servicesMapByName       map[string][]*metadata.Service
	hosts                   []metadata.Host
	containers              []metadata.Container
	hostsMap                map[string]metadata.Host
	networksMap             map[string]metadata.Network
}

// NewMetadataStoreWithClientIP creates, intializes and returns a store for use with a specific Client IP to contact the metadata
func NewMetadataStoreWithClientIP(metadataAddress, clientIP string) (*MetadataStore, error) {
	if metadataAddress == "" {
		metadataAddress = DefaultMetadataAddress
	}
	metadataURL := fmt.Sprintf(metadataURLTemplate, metadataAddress)

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
func NewMetadataStore(metadataAddress string) (*MetadataStore, error) {
	if metadataAddress == "" {
		metadataAddress = DefaultMetadataAddress
	}
	metadataURL := fmt.Sprintf(metadataURLTemplate, metadataAddress)

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

// LocalHostIPAddress returns the IP address of the host where the agent is running
func (ms *MetadataStore) LocalHostIPAddress() string {
	return ms.self.HostIPAddress
}

// LocalIPAddress returns the IP address of the current agent
func (ms *MetadataStore) LocalIPAddress() string {
	ip, _, err := net.ParseCIDR(ms.self.IPAddress)
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

	isSelf := (c.PrimaryIp == ms.info.selfContainer.PrimaryIp)
	isPeer := false

	entry := Entry{
		c.PrimaryIp + ms.info.selfNetworkSubnetPrefix,
		ms.info.hostsMap[c.HostUUID].AgentIP,
		isSelf,
		isPeer,
	}

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

// RemoteNonPeerEntriesMap is used to get a map of all entries which are remote
func (ms *MetadataStore) RemoteNonPeerEntriesMap() map[string]Entry {
	return ms.remoteNonPeersMap
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

func getNetworksMapFromNetworksArray(networks []metadata.Network) map[string]metadata.Network {
	networksMap := map[string]metadata.Network{}

	for _, aNetwork := range networks {
		networksMap[aNetwork.UUID] = aNetwork
	}

	logrus.Debugf("networksMap: %+v", networksMap)
	return networksMap
}

func (ms *MetadataStore) getLinkedFromServicesToSelf() []*metadata.Service {
	linkedTo := ms.info.selfService.StackName + "/" + ms.info.selfService.Name
	logrus.Debugf("getLinkedFromServicesToSelf linkedTo: %v", linkedTo)

	var linkedFromServices []*metadata.Service

	for _, service := range ms.info.services {
		if !service.System {
			continue
		}
		linkedFromServiceName := service.StackName + "/" + service.Name
		if len(service.Links) > 0 {
			for linkedService := range service.Links {
				if linkedService != linkedTo {
					continue
				}
				linkedFromServices = append(linkedFromServices, ms.info.servicesMapByName[linkedFromServiceName]...)
			}
		}
	}

	logrus.Debugf("linkedFromServices: %v", linkedFromServices)
	return linkedFromServices
}

// When environments are linked, the network services across the
// environments are linked. This function goes through the links
// either to/from and figures out the networks of those peers.
func (ms *MetadataStore) getLinkedPeersInfo() (map[string]bool, []metadata.Container) {
	linkedPeersNetworks := map[string]bool{}
	var linkedPeersContainers []metadata.Container

	// Find out if the current service has links else if other services link to current service
	if len(ms.info.selfService.Links) > 0 {
		for linkedServiceName := range ms.info.selfService.Links {
			linkedServices, ok := ms.info.servicesMapByName[linkedServiceName]
			logrus.Debugf("linkedServices: %+v", linkedServices)
			if !ok {
				logrus.Errorf("Current service is linked to service: %v, but cannot find in servicesMapByName", linkedServiceName)
				continue
			} else {
				for _, aService := range linkedServices {
					for _, aContainer := range aService.Containers {
						if !(aContainer.State == "running" || aContainer.State == "starting") {
							continue
						}
						// Skip containers whose network names don't match self
						if ms.info.networksMap[aContainer.NetworkUUID].Name != ms.info.selfNetwork.Name {
							continue
						}
						linkedPeersContainers = append(linkedPeersContainers, aContainer)
						if _, ok := linkedPeersNetworks[aContainer.NetworkUUID]; !ok {
							linkedPeersNetworks[aContainer.NetworkUUID] = true
						}
					}
				}
			}
		}
	} else {
		linkedFromServices := ms.getLinkedFromServicesToSelf()
		for _, aService := range linkedFromServices {
			for _, aContainer := range aService.Containers {
				if !(aContainer.State == "running" || aContainer.State == "starting") {
					continue
				}
				// Skip containers whose network names don't match self
				if ms.info.networksMap[aContainer.NetworkUUID].Name != ms.info.selfNetwork.Name {
					continue
				}
				linkedPeersContainers = append(linkedPeersContainers, aContainer)
				if _, ok := linkedPeersNetworks[aContainer.NetworkUUID]; !ok {
					linkedPeersNetworks[aContainer.NetworkUUID] = true
				}
			}
		}
	}

	logrus.Debugf("getLinkedPeersInfo linkedPeersNetworks: %+v", linkedPeersNetworks)
	logrus.Debugf("getLinkedPeersInfo linkedPeersContainers: %v", linkedPeersContainers)
	return linkedPeersNetworks, linkedPeersContainers
}

func (ms *MetadataStore) doInternalRefresh() {
	logrus.Debugf("Doing internal refresh")

	ms.self, _ = ms.getEntryFromContainer(ms.info.selfContainer)

	seen := map[string]bool{}
	entries := []Entry{}
	local := map[string]Entry{}
	remote := map[string]Entry{}
	peersMap := map[string]Entry{}
	remoteNonPeersMap := map[string]Entry{}
	peersNetworks, linkedPeersContainers := ms.getLinkedPeersInfo()

	// Add self network to peersNetworks
	peersNetworks[ms.info.selfContainer.NetworkUUID] = true

	var allPeersContainers []metadata.Container
	allPeersContainers = append(allPeersContainers, linkedPeersContainers...)
	for _, c := range ms.info.selfService.Containers {
		if c.State == "running" || c.State == "starting" {
			allPeersContainers = append(allPeersContainers, c)
		}
	}

	for _, sc := range allPeersContainers {
		e, _ := ms.getEntryFromContainer(sc)
		e.Peer = true
		ipNoCidr := strings.Split(e.IPAddress, "/")[0]
		peersMap[ipNoCidr] = e
	}

	for _, c := range ms.info.containers {
		if !(c.State == "running" || c.State == "starting") {
			continue
		}

		// check if the container networkUUID is part of peersNetworks
		_, isPresentInPeersNetworks := peersNetworks[c.NetworkUUID]

		if !isPresentInPeersNetworks ||
			c.PrimaryIp == "" ||
			c.NetworkFromContainerUUID != "" {
			continue
		}

		logrus.Debugf("Getting Entry from Container: %+v", c)
		e, _ := ms.getEntryFromContainer(c)

		ipNoCidr := strings.Split(e.IPAddress, "/")[0]

		if seen[ipNoCidr] {
			continue
		}
		seen[ipNoCidr] = true

		if _, ok := peersMap[ipNoCidr]; ok {
			e.Peer = true
		}

		if e.HostIPAddress == ms.self.HostIPAddress {
			local[ipNoCidr] = e
		} else {
			remote[ipNoCidr] = e
			if !e.Peer {
				remoteNonPeersMap[ipNoCidr] = e
			}
		}

		logrus.Debugf("entry: %+v", e)
		entries = append(entries, e)
	}

	logrus.Debugf("entries: %+v", entries)
	logrus.Debugf("peersMap: %+v", peersMap)
	logrus.Debugf("local: %+v", local)
	logrus.Debugf("remote: %+v", remote)

	ms.entries = entries
	ms.peersMap = peersMap
	ms.local = local
	ms.remote = remote
	ms.remoteNonPeersMap = remoteNonPeersMap
}

// getServicesMapByName builds a map indexed by `stack_name/service_name`
// It excludes the current service in the map
func getServicesMapByName(services []metadata.Service, selfService metadata.Service) map[string][]*metadata.Service {
	// Build serviceMap by "stack_name/service_name"
	// The reason for an array in map value is because of not
	// using UUID but names which can result in duplicates.
	// TODO: Once LinksByUUID is available, use that instead
	servicesMapByName := make(map[string][]*metadata.Service)
	for index, aService := range services {
		if !aService.System || aService.UUID == selfService.UUID {
			continue
		}
		key := aService.StackName + "/" + aService.Name
		if value, ok := servicesMapByName[key]; ok {
			servicesMapByName[key] = append(value, &services[index])

		} else {
			servicesMapByName[key] = []*metadata.Service{&services[index]}
		}
	}
	logrus.Debugf("servicesMapByName: %+v", servicesMapByName)

	return servicesMapByName
}

func getSubnetPrefixFromNetworkConfig(network metadata.Network) string {
	conf, _ := network.Metadata["cniConfig"].(map[string]interface{})
	for _, file := range conf {
		props, _ := file.(map[string]interface{})
		ipamConf, found := props["ipam"].(map[string]interface{})
		if !found {
			logrus.Errorf("couldn't find ipam key in network config")
			return defaultSubnetPrefix
		}

		sp, found := ipamConf["subnetPrefixSize"].(string)
		if !found {
			logrus.Errorf("couldn't find subnetPrefixSize in network ipam config")
			return defaultSubnetPrefix
		}
		return sp
	}
	return defaultSubnetPrefix
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

	services, err := ms.mc.GetServices()
	if err != nil {
		logrus.Errorf("couldn't get services from metadata: %v", err)
		return err
	}

	servicesMapByName := getServicesMapByName(services, selfService)

	hostsMap := getHostsMapFromHostsArray(hosts)

	networks, err := ms.mc.GetNetworks()
	if err != nil {
		logrus.Errorf("couldn't get networks from metadata: %v", err)
		return err
	}
	networksMap := getNetworksMapFromNetworksArray(networks)

	selfNetwork, ok := networksMap[selfContainer.NetworkUUID]
	if !ok {
		return fmt.Errorf("couldn't find self network in metadata")
	}

	selfNetworkSubnetPrefix := getSubnetPrefixFromNetworkConfig(selfNetwork)

	info := &InfoFromMetadata{
		selfContainer,
		selfHost,
		selfService,
		selfNetwork,
		selfNetworkSubnetPrefix,
		services,
		servicesMapByName,
		hosts,
		containers,
		hostsMap,
		networksMap,
	}

	ms.info = info

	ms.doInternalRefresh()

	return nil
}
