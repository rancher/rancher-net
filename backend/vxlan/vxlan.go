// Package vxlan provides the capabilities to create a VXLAN overlay network
package vxlan

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
)

const (
	changeCheckInterval = 5
	metadataURLTemplate = "http://%v/2015-12-19"
	ipLabel             = "io.rancher.container.ip"

	vxlanInterfaceName = "vtep1042"
	vxlanVni           = 1042
	vxlanMACRange      = "0E:00:00:00:00:00"
	vxlanPort          = 4789
	//vxlanPort          = 46354 //There is a bug in netlink library 46354 ~ swapped 4789

	emptyIPAddress = ""

	// DefaultMetadataAddress specifies the default value to use if nothing is specified
	DefaultMetadataAddress = "169.254.169.250"

	DefaultVTEPMTU = 1500
)

// Overlay is used to store the VXLAN overlay information
type Overlay struct {
	mu sync.Mutex
	m  metadata.Client
	v  *vxlanIntfInfo

	local  map[string]bool
	remote map[string]bool

	vtepMTU int
}

// NewOverlay is used to create a new VXLAN Overlay network
func NewOverlay(vtepMTU int) (*Overlay, error) {
	metadataAddress := os.Getenv("RANCHER_METADATA_ADDRESS")
	if metadataAddress == "" {
		metadataAddress = DefaultMetadataAddress
	}
	metadataURL := fmt.Sprintf(metadataURLTemplate, metadataAddress)

	logrus.Debugf("Creating new VXLAN Overlay, metadataURL: %v", metadataURL)
	m, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		logrus.Errorf("couldn't create metadata client: %v", err)
		return nil, err
	}

	o := &Overlay{
		m:       m,
		vtepMTU: vtepMTU,
	}

	o.v, err = o.getDefaultVxlanInterfaceInfo()
	if err != nil {
		logrus.Errorf("vxlan: couldn't get default vxlan inteface info: %v", err)
		return nil, nil
	}

	return o, nil
}

// Start is used to start the vxlan overlay
func (o *Overlay) Start(launch bool, logFile string) {
	logrus.Infof("vxlan: Start")
	logrus.Debugf("launch: %v", launch)

	err := disableChecksumOffload()
	if err != nil {
		logrus.Errorf("vxlan: Start: error disabling tx checksum offload")
		return
	}

	err = o.configure()
	if err != nil {
		logrus.Errorf("couldn't configure: %v", err)
		logrus.Errorf("vxlan: Start: failed")
	} else {
		logrus.Infof("vxlan: Start: success")
	}

	go o.m.OnChange(changeCheckInterval, o.onChangeNoError)
}

func (o *Overlay) onChangeNoError(version string) {
	if err := o.Reload(); err != nil {
		logrus.Errorf("Failed to apply VXLAN rules: %v", err)
	}
}

// Reload does a db reload and reconfigures the configuration
// with the new data
func (o *Overlay) Reload() error {
	logrus.Infof("vxlan: Reload")
	err := o.configure()
	if err != nil {
		logrus.Errorf("vxlan: Reload: couldn't configure: %v", err)
	}
	return err
}

func (o *Overlay) configure() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	logrus.Infof("vxlan: configure")
	var (
		routesMap    = make(map[string]*net.IPNet)       // {ContainerIP: ipnet}
		arpMap       = make(map[string]net.HardwareAddr) // {ContainerIP: mac}
		fdbMap       = make(map[string]net.HardwareAddr) // {HostIP: mac}
		peersHostMap = make(map[string]string)           // {HostUUID: peerContainerIP}
	)
	o.local = make(map[string]bool)
	o.remote = make(map[string]bool)

	selfHost, err := o.m.GetSelfHost()
	if err != nil {
		logrus.Errorf("Couldn't get self host from metadata: %v", err)
		return err
	}
	selfContainer, err := o.m.GetSelfContainer()
	if err != nil {
		logrus.Errorf("Couldn't get self container from metadata: %v", err)
		return err
	}
	o.local[selfContainer.PrimaryIp] = true
	selfService, err := o.m.GetSelfService()
	if err != nil {
		logrus.Errorf("Couldn't get self service from metadata: %v", err)
		return err
	}
	allServices, err := o.m.GetServices()
	if err != nil {
		logrus.Errorf("Couldn't get self service from metadata: %v", err)
		return err
	}
	allContainers, err := o.m.GetContainers()
	if err != nil {
		logrus.Errorf("Couldn't get containers from metadata: %v", err)
		return err
	}
	networks, err := o.m.GetNetworks()
	if err != nil {
		logrus.Errorf("Couldn't get networks from metadata: %v", err)
		return err
	}
	networksMap := getNetworksMap(networks)
	selfNetwork, ok := networksMap[selfContainer.NetworkUUID]
	if !ok {
		return fmt.Errorf("Couldn't find self network in metadata")
	}
	hosts, err := o.m.GetHosts()
	if err != nil {
		logrus.Errorf("Couldn't get hosts from metadata: %v", err)
		return err
	}
	hostsMap := getHostsMap(hosts)

	// First create the local VTEP interface
	err = o.checkAndCreateVTEP()
	if err != nil {
		logrus.Errorf("Error creating VTEP interface")
		return err
	}

	peersNetworks, linkedPeersContainers := getLinkedPeersInfo(allServices, selfService, networksMap, selfNetwork)

	// Add self network to peersNetworks
	peersNetworks[selfContainer.NetworkUUID] = true

	var allPeersContainers []metadata.Container
	allPeersContainers = append(allPeersContainers, linkedPeersContainers...)
	for _, c := range selfService.Containers {
		if c.State == "running" || c.State == "starting" {
			allPeersContainers = append(allPeersContainers, c)
		}
	}

	for _, c := range allPeersContainers {
		if c.HostUUID == selfHost.UUID {
			continue
		}
		ip := net.ParseIP(c.PrimaryIp)
		_, ipnet, err := net.ParseCIDR(c.PrimaryIp + "/32")
		if err != nil {
			logrus.Errorf("Failed to parseCIDR in peersContainers: %v", err)
			continue
		}
		peerMAC, err := getMACAddressForVxlanIP(vxlanMACRange, ip)
		if err != nil {
			logrus.Errorf("Failed to ParseMAC in peersContainers: %v", err)
			continue
		}
		hostIPAddress := hostsMap[c.HostUUID].AgentIP

		routesMap[ip.To4().String()] = ipnet
		arpMap[ip.To4().String()] = peerMAC
		fdbMap[hostIPAddress] = peerMAC

		peersHostMap[c.HostUUID] = ip.To4().String()
	}

	logrus.Debugf("Get peersHostMap: %v", peersHostMap)

	for _, c := range allContainers {
		if !(c.State == "running" || c.State == "starting") {
			continue
		}
		// check if the container networkUUID is part of peersNetworks
		_, isPresentInPeersNetworks := peersNetworks[c.NetworkUUID]

		if !isPresentInPeersNetworks ||
			c.PrimaryIp == "" ||
			c.NetworkFromContainerUUID != "" ||
			c.HostUUID == selfHost.UUID {
			continue
		}

		o.remote[c.PrimaryIp] = true

		_, ipnet, err := net.ParseCIDR(c.PrimaryIp + "/32")
		if err != nil {
			logrus.Errorf("Failed to parseCIDR in nonPeersContainers: %v", err)
			continue
		}
		peerIPAddress, ok := peersHostMap[c.HostUUID]
		if !ok || c.PrimaryIp == peerIPAddress {
			// skip peer containers
			continue
		}
		peerIP := net.ParseIP(peersHostMap[c.HostUUID])
		peerMAC, err := getMACAddressForVxlanIP(vxlanMACRange, peerIP)
		if err != nil {
			logrus.Errorf("Failed to ParseMAC in nonPeersContainers: %v", err)
			continue
		}

		routesMap[c.PrimaryIp] = ipnet
		arpMap[c.PrimaryIp] = peerMAC
	}

	vtepLink, err := getNetLink(o.v.name)
	if err != nil {
		return err
	}
	currentRouteEntries, err := getCurrentRouteEntries(vtepLink)
	if err != nil {
		return err
	}
	err = updateRoute(currentRouteEntries, getDesiredRouteEntries(vtepLink, routesMap))
	if err != nil {
		return err
	}
	currentARPEntries, err := getCurrentARPEntries(vtepLink)
	if err != nil {
		return err
	}
	err = updateARP(currentARPEntries, getDesiredARPEntries(vtepLink, arpMap))
	if err != nil {
		return err
	}
	currentFDBEntries, err := getCurrentFDBEntries(vtepLink)
	if err != nil {
		return err
	}
	err = updateFDB(currentFDBEntries, getDesiredFDBEntries(vtepLink, fdbMap))
	if err != nil {
		return err
	}

	return nil
}

// GetMyVTEPInfo is used to figure out the MAC address to be assigned
// for the VTEP address.
func (o *Overlay) GetMyVTEPInfo() (net.HardwareAddr, error) {
	logrus.Debugf("vxlan: GetMyVTEPInfo")

	selfContainer, err := o.m.GetSelfContainer()
	if err != nil {
		logrus.Errorf("Couldn't get self container from metadata: %v", err)
		return nil, err
	}
	myRancherIPString := selfContainer.PrimaryIp
	myRancherIP := net.ParseIP(myRancherIPString)
	logrus.Debugf("myRancherIP: %v", myRancherIPString)
	mac, err := getMACAddressForVxlanIP(vxlanMACRange, myRancherIP)
	if err != nil {
		return nil, err
	}

	logrus.Debugf("vxlan: my vtep info mac:%v", mac)
	return mac, nil
}

func (o *Overlay) getDefaultVxlanInterfaceInfo() (*vxlanIntfInfo, error) {
	logrus.Debugf("vxlan: getDefaultVxlanInterfaceInfo")
	mac, err := o.GetMyVTEPInfo()
	if err != nil {
		logrus.Errorf("Error: %v", err)
		return nil, err
	}

	return &vxlanIntfInfo{
		name: vxlanInterfaceName,
		vni:  vxlanVni,
		port: vxlanPort,
		mac:  mac,
		mtu:  o.vtepMTU,
	}, nil
}

// CreateVTEP creates a vxlan interface with the default values
func (o *Overlay) CreateVTEP() error {
	logrus.Debugf("vxlan: CreateVTEP")
	logrus.Debugf("vxlan: trying to create vtep: %v", o.v)
	err := createVxlanInterface(o.v)
	if err != nil {
		// The errors are really mysterious, hence
		// documenting the ones I came across.
		// invalid argument:
		//   Could mean there is another interface with similar properties.
		logrus.Errorf("Error creating vxlan interface v=%v: err=%v", o.v, err)
		return err
	}

	return nil
}

// DeleteVTEP deletes a vxlan interface with the default name
func (o *Overlay) DeleteVTEP() error {
	return deleteVxlanInterface(o.v.name)
}

func (o *Overlay) checkAndCreateVTEP() error {
	logrus.Debugf("vxlan: checkAndCreateVTEP")

	l, err := findVxlanInterface(o.v.name)
	if err != nil {
		return o.CreateVTEP()
	}

	if l == nil {
		return errors.New("couldn't find link and didn't get error")
	}
	return nil
}

func (o *Overlay) checkAndDeleteVTEP() error {
	_, err := findVxlanInterface(o.v.name)
	if err == nil {
		return o.DeleteVTEP()
	}

	return nil
}

// IsRemote is used to check if the given IP address is remote to the current host
func (o *Overlay) IsRemote(ipAddress string) bool {
	if _, ok := o.local[ipAddress]; ok {
		logrus.Debugf("Local: %s", ipAddress)
		return false
	}

	_, ok := o.remote[ipAddress]
	if ok {
		logrus.Debugf("Remote: %s", ipAddress)
	}
	return ok
}
