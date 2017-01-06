// Package vxlan provides the capabilities to create a VXLAN overlay network
package vxlan

import (
	"errors"
	"net"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/rancher-net/store"
	"github.com/vishvananda/netlink"
)

const (
	metadataURL = "http://rancher-metadata/2015-12-19"

	vxlanInterfaceName = "vtep1042"
	vxlanVni           = 1042
	vxlanIPSubnet      = "169.254.0.0"
	vxlanMACRange      = "00:AB:A9:FE:00:00"
	vxlanMTU           = 1400
	vxlanPort          = 4789
	//vxlanPort          = 46354 //There is a bug in netlink library 46354 ~ swapped 4789

	// Corresponds to 169.254.169.254
	blackListIndex    = 43518
	allowedStartIndex = 1
	allowedEndIndex   = 65533
	octetSize         = 256

	emptyIPAddress = ""
)

// Operation specifies if it's an add/delete/update
type Operation int

// ADD/DELETE/UPDATE operations
const (
	ADD Operation = iota
	DELETE
	UPDATE
)

// Overlay is used to store the VXLAN overlay information
type Overlay struct {
	sync.Mutex

	m                 metadata.Client
	db                store.Store
	peersMapping      map[string]net.IP
	prevPeerEntries   map[string]store.Entry
	prevRemoteEntries map[string]store.Entry
	v                 *vxlanIntfInfo
}

type peerVxlanEntry struct {
	intfName string
	ip       net.IP
	vtepIP   net.IP
	vtepMAC  net.HardwareAddr
	hostIP   net.IP
}

type remoteVxlanEntry struct {
	ip  *net.IPNet
	via net.IP
}

type vxlanIntfInfo struct {
	name string
	vni  int
	port int
	ip   net.IP
	mac  net.HardwareAddr
	mtu  int
}

type entriesDiff struct {
	toAdd map[string]store.Entry
	toDel map[string]store.Entry
	toUpd map[string]store.Entry
}

// NewTestOverlay is used to create a new VXLAN Overlay network
// for testing purposes
func NewTestOverlay(v *vxlanIntfInfo, db store.Store) (*Overlay, error) {
	logrus.Debugf("vxlan: creating new overlay: %+v", v)
	mc, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		return nil, err
	}

	return &Overlay{
		m:  mc,
		db: db,
		v:  v,
	}, nil
}

// NewOverlay is used to create a new VXLAN Overlay network
func NewOverlay(configDir string, db store.Store) (*Overlay, error) {
	logrus.Debugf("vxlan: creating new overlay db=%+v", db)
	o := &Overlay{
		db: db,
	}

	var err error
	o.v, err = o.getDefaultVxlanInterfaceInfo()
	if err != nil {
		logrus.Errorf("vxlan: couldn't get default vxlan inteface info: %v", err)
		return nil, nil
	}

	return o, nil
}

func disableChecksumOffload() error {
	logrus.Infof("vxlan: disabling tx checksum offload")

	cmdOutput, err := exec.Command("ethtool", "-K", "eth0", "tx", "off").CombinedOutput()
	if err != nil {
		logrus.Errorf("err: %v, cmdOut=%v", err, string(cmdOutput))
		return err
	}

	return nil
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
}

// Reload does a db reload and reconfigures the configuration
// with the new data
func (o *Overlay) Reload() error {
	logrus.Infof("vxlan: Reload")
	if err := o.db.Reload(); err != nil {
		return err
	}

	err := o.configure()
	if err != nil {
		logrus.Errorf("vxlan: Reload: couldn't configure: %v", err)
	}
	return err
}

func (o *Overlay) configure() error {
	//o.Lock()
	//defer o.Unlock()
	logrus.Infof("vxlan: configure")

	// First create the local VTEP interface
	err := o.checkAndCreateVTEP()
	if err != nil {
		logrus.Errorf("Error creating VTEP interface")
		return err
	}

	entries := o.db.PeerEntriesMap()
	logrus.Debugf("entries: %v", entries)

	o.peersMapping = buildPeersMapping(entries)

	var firstErr error
	//localHostIP := o.db.LocalHostIpAddress()

	// Install/Update/Delete peer entries
	currentPeerEntries := o.db.PeerEntriesMap()
	peerEntriesDiff := calculateDiffOfEntries(o.prevPeerEntries, currentPeerEntries)
	o.handlePeerEntries(peerEntriesDiff)
	o.prevPeerEntries = currentPeerEntries

	// Install/Update/Delete remote entries
	currentRemoteEntries := o.db.RemoteEntriesMap()
	remoteEntriesDiff := calculateDiffOfEntries(o.prevRemoteEntries, currentRemoteEntries)
	handleNonPeerRemoteEntries(remoteEntriesDiff, o.peersMapping)
	o.prevRemoteEntries = currentRemoteEntries

	return firstErr
}

func (o *Overlay) cleanup() error {
	//o.Lock()
	//defer o.Unlock()
	logrus.Infof("vxlan: cleanup")

	currentRemoteEntries := o.db.RemoteEntriesMap()
	remoteEntriesOperation(currentRemoteEntries, DELETE, o.peersMapping)

	currentPeerEntries := o.db.PeerEntriesMap()
	o.peerEntriesOperation(currentPeerEntries, DELETE)

	err := o.checkAndDeleteVTEP()
	if err != nil {
		logrus.Errorf("Error deleting VTEP interface")
		return err
	}

	return nil
}

// buildPeersMapping takes the entries from db and builds a map
// to lookup what Network Agent/peer is running behind a host.
// Host IP address -> VXLAN range mapped IP address of the N.A
func buildPeersMapping(entries map[string]store.Entry) map[string]net.IP {
	logrus.Debugf("vxlan: buildPeersMapping")
	logrus.Debugf("entries: %v", entries)

	peersMapping := make(map[string]net.IP)

	for _, entry := range entries {
		if entry.Peer && !entry.Self {
			entryIP, _, err := net.ParseCIDR(entry.IpAddress)
			if err != nil {
				logrus.Errorf("err: %#v", err)
				continue
			}
			vxlanIP, err := mapRancherIPToVxlanIP(entryIP)
			if err != nil {
				logrus.Errorf("couldn't map ip: %v", err)
				continue
			}
			peersMapping[entry.HostIpAddress] = vxlanIP
		}
	}

	logrus.Debugf("peersMapping: %#v", peersMapping)
	return peersMapping
}

// newPeerVxlanEntry creates a new struct representing the Peer
// from the given entry
func newPeerVxlanEntry(intfName string, e store.Entry) *peerVxlanEntry {
	ip, _, err := net.ParseCIDR(e.IpAddress)
	if err != nil {
		logrus.Errorf("Couldn't parseCIDR for IP: %v", e.IpAddress)
		return nil
	}

	vtepIP, err := mapRancherIPToVxlanIP(ip)
	if err != nil {
		logrus.Errorf("Couldn't get vxlan IP for: %v", ip)
		return nil
	}

	vtepMAC, err := getMACAddressForVxlanIP(ip)
	if err != nil {
		logrus.Errorf("Couldn't get MAC address for IP: %v", ip)
		return nil
	}
	hostIP := net.ParseIP(e.HostIpAddress)

	return &peerVxlanEntry{intfName, ip, vtepIP, vtepMAC, hostIP}
}

func (v *peerVxlanEntry) add() error {
	var err error

	err = addARPEntry(v.intfName, v.vtepIP, v.vtepMAC)
	if err != nil {
		return err
	}

	err = addVxlanForwardingEntry(v.intfName, v.vtepMAC, v.hostIP)
	if err != nil {
		delARPEntry(v.intfName, v.vtepIP, v.vtepMAC)
		return err
	}

	return nil
}

func (v *peerVxlanEntry) del() error {
	var err error

	err = delARPEntry(v.intfName, v.vtepIP, v.vtepMAC)
	if err != nil {
		return err
	}

	err = deleteVxlanForwardingEntry(v.intfName, v.vtepMAC, v.hostIP)
	if err != nil {
		return err
	}

	return nil

}

func (v *peerVxlanEntry) upd() error {
	return nil
}

// newRemoteVxlanEntry create a new struct representing the
// non peer remote entry
// Need to change /16 to /32, else the route is not getting installed
func newRemoteVxlanEntry(e store.Entry, peersMapping map[string]net.IP) *remoteVxlanEntry {
	logrus.Debugf("building remoteVxlanEntry for %v", e)

	ipslash32 := strings.Split(e.IpAddress, "/")[0] + "/32"

	ip, err := netlink.ParseIPNet(ipslash32)
	if err != nil {
		logrus.Errorf("Couldn't ParseIPNet for IP: %v", e.IpAddress)
		return nil
	}

	via := peersMapping[e.HostIpAddress]

	return &remoteVxlanEntry{ip, via}
}

func (v *remoteVxlanEntry) add() error {
	err := addRoute(v.ip, v.via)
	if err != nil {
		return err
	}

	return nil
}

func (v *remoteVxlanEntry) del() error {
	err := delRoute(v.ip, v.via)
	if err != nil {
		return err
	}

	return nil
}

func (v *remoteVxlanEntry) upd() error {
	return nil
}

func (o *Overlay) peerEntriesOperation(entries map[string]store.Entry, op Operation) {
	for _, e := range entries {
		v := newPeerVxlanEntry(o.v.name, e)
		if v == nil {
			logrus.Errorf("Got nil for e: %v", e)
			continue
		}

		switch op {
		case ADD:
			logrus.Debugf("vxlan: Adding peer: %+v", *v)
			v.add()
		case DELETE:
			logrus.Debugf("vxlan: Deleting peer: %+v", *v)
			v.del()
		case UPDATE:
			logrus.Debugf("vxlan: Updating peer: %+v", *v)
			v.upd()
		}
	}
}

// handlePeerEntries takes care of installing the ARP and bridge entry
// Peer = Network Agent
func (o *Overlay) handlePeerEntries(diff entriesDiff) {
	o.peerEntriesOperation(diff.toAdd, ADD)
	o.peerEntriesOperation(diff.toDel, DELETE)
	o.peerEntriesOperation(diff.toUpd, UPDATE)

}

func remoteEntriesOperation(entries map[string]store.Entry, op Operation, peersMapping map[string]net.IP) {
	for _, e := range entries {
		v := newRemoteVxlanEntry(e, peersMapping)
		if v == nil {
			logrus.Errorf("Got nil for e: %v", e)
			continue
		}

		switch op {
		case ADD:
			logrus.Debugf("vxlan: Adding non peer remote : %+v", *v)
			v.add()
		case DELETE:
			logrus.Debugf("vxlan: Deleting non peer remote : %+v", *v)
			v.del()
		case UPDATE:
			logrus.Debugf("vxlan: Updating non peer remote : %+v", *v)
			v.upd()
		}
	}
}

func handleNonPeerRemoteEntries(diff entriesDiff, peersMapping map[string]net.IP) {
	remoteEntriesOperation(diff.toAdd, ADD, peersMapping)
	remoteEntriesOperation(diff.toDel, DELETE, peersMapping)
	remoteEntriesOperation(diff.toUpd, UPDATE, peersMapping)
}

// getIPAddressInVxlanSubnet is used to get an IP address in the
// subnet range based on index. Since we use 169.254.169.254, for that
// particular index we just use the last IP address
func getIPAddressInVxlanSubnet(index int) (ip net.IP, err error) {
	logrus.Debugf("vxlan: index: %v", index)
	if index < allowedStartIndex || index > allowedEndIndex {
		err := errors.New("vxlan: index out of range (1-65534)")
		return nil, err
	}

	if index == blackListIndex {
		index = allowedEndIndex + 1
	}

	vxlanSubnetBaseIP := net.ParseIP(vxlanIPSubnet)
	vxlanSubnetBaseIPTo4 := vxlanSubnetBaseIP.To4()

	fourthOctetOfIndex := index % octetSize
	thirdOctetOfIndex := index / octetSize

	ipForIndex := net.IPv4(
		vxlanSubnetBaseIPTo4[0],
		vxlanSubnetBaseIPTo4[1],
		byte(thirdOctetOfIndex),
		byte(fourthOctetOfIndex),
	)
	logrus.Debugf("vxlan: ipForIndex: %v", ipForIndex)

	return ipForIndex, nil
}

// mapRancherIPToVxlanIP takes a 10.42.X.Y ip address and
// returns 169.254.X.Y ip address.
func mapRancherIPToVxlanIP(rancherIP net.IP) (net.IP, error) {
	if rancherIP == nil {
		return nil, errors.New("Rancher IP is nil")
	}

	rancherIPTo4 := rancherIP.To4()

	vxlanSubnetBaseIP := net.ParseIP(vxlanIPSubnet)
	vxlanSubnetBaseIPTo4 := vxlanSubnetBaseIP.To4()

	vxlanIP := net.IPv4(
		vxlanSubnetBaseIPTo4[0],
		vxlanSubnetBaseIPTo4[1],
		rancherIPTo4[2],
		rancherIPTo4[3],
	)
	logrus.Debugf("vxlan: for Rancher IP: %v, vxlanIP: %v", rancherIP, vxlanIP)

	return vxlanIP, nil
}

func getMyRancherIPFromInterface() (net.IP, error) {
	eth0, err := net.InterfaceByName("eth0")
	if err != nil {
		logrus.Errorf("Couldn't get interface eth0")
		return nil, err
	}

	for i := 0; i < 60; i++ {
		ips, err := eth0.Addrs()
		if err != nil {
			logrus.Errorf("Coudln't get ip address for eth0: %v", err)
			return nil, err
		}
		for _, ip := range ips {
			if strings.HasPrefix(ip.String(), "10.42") {
				logrus.Debugf("Found Rancher IP: %v", ip)
				i, _, _ := net.ParseCIDR(ip.String())
				return i, nil
			}
		}
		logrus.Infof("Waiting for Rancher IP")
		time.Sleep(1 * time.Second)
	}

	logrus.Debugf("Rancher IP not found")
	return nil, errors.New("Rancher IP not found")
}

func (o *Overlay) getMyVxlanIPBasedOnRancherIP() (net.IP, error) {
	logrus.Debugf("vxlan: getMyVxlanIPBasedOnRancherIP")
	myRancherIPString := o.db.LocalIpAddress()
	logrus.Debugf("myRancherIP: %v", myRancherIPString)

	myRancherIP := net.ParseIP(myRancherIPString)
	vxlanIP, err := mapRancherIPToVxlanIP(myRancherIP)
	if err != nil {
		return nil, err
	}

	return vxlanIP, nil
}

// GetMyVTEPInfo is used to figure out the IP address to be assigned
// for the VTEP address. The IP address is assigned from the 169.254.0.0/16
// subnet. We get the hostId from the metadata and append it to the subnet.
func (o *Overlay) GetMyVTEPInfo() (net.IP, net.HardwareAddr, error) {
	logrus.Debugf("vxlan: GetMyVTEPInfo")
	ip, err := o.getMyVxlanIPBasedOnRancherIP()
	if err != nil {
		return nil, nil, err
	}

	mac, err := getMACAddressForVxlanIP(ip)
	if err != nil {
		return nil, nil, err
	}

	logrus.Debugf("vxlan: my vtep info ip: %v, mac:%v", ip, mac)
	return ip, mac, nil
}

func (o *Overlay) getDefaultVxlanInterfaceInfo() (*vxlanIntfInfo, error) {
	logrus.Debugf("vxlan: getDefaultVxlanInterfaceInfo")
	ip, mac, err := o.GetMyVTEPInfo()
	if err != nil {
		logrus.Errorf("Error: %v", err)
		return nil, err
	}

	return &vxlanIntfInfo{
		name: vxlanInterfaceName,
		vni:  vxlanVni,
		port: vxlanPort,
		ip:   ip,
		mac:  mac,
		mtu:  vxlanMTU,
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

// getMACAddressForVxlanIP takes the input Vxlan IP (169.254.X.Y) and
// builds a MAC address 00:AB:169:254:X:Y (Actual in hex: 00:AB:A9:FE:X:Y)
func getMACAddressForVxlanIP(ip net.IP) (net.HardwareAddr, error) {
	mac, err := net.ParseMAC(vxlanMACRange)
	if err != nil {
		return nil, err
	}

	ip4 := ip.To4()
	mac[5] = ip4[3]
	mac[4] = ip4[2]

	return mac, nil
}

func createVxlanInterface(v *vxlanIntfInfo) error {
	logrus.Debugf("vxlan: creating interface %+v", v)

	vxlan := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{Name: v.name, MTU: v.mtu},
		VxlanId:   v.vni,
		Learning:  false,
		Port:      v.port,
		Proxy:     true,
		L3miss:    true,
		L2miss:    true,
		RSC:       true,
	}

	err := netlink.LinkAdd(vxlan)
	if err != nil && err != syscall.EEXIST {
		return err
	}

	err = netlink.LinkSetHardwareAddr(vxlan, v.mac)
	if err != nil {
		return err
	}

	i := &net.IPNet{IP: v.ip, Mask: net.IPv4Mask(255, 255, 0, 0)}
	ipAddr := &netlink.Addr{IPNet: i, Label: ""}
	err = netlink.AddrAdd(vxlan, ipAddr)
	if err != nil {
		logrus.Errorf("vxlan: failed to add address to the interface: %v", err)
		return err
	}

	err = netlink.LinkSetUp(vxlan)
	if err != nil {
		logrus.Errorf("setting link up got error: %v", err)
		return err
	}

	return nil
}

func deleteVxlanInterface(name string) error {
	logrus.Debugf("vxlan: deleting interface %v", name)

	link, err := netlink.LinkByName(name)
	if err != nil {
		logrus.Errorf("vxlan: failed to find interface with name %s: %v", name, err)
		return err
	}

	if err := netlink.LinkDel(link); err != nil {
		logrus.Errorf("vxlan: error deleting interface with name %s: %v", name, err)
		return err
	}

	return nil
}

func findVxlanInterface(name string) (netlink.Link, error) {
	link, err := netlink.LinkByName(name)

	if err != nil {
		return nil, err
	}

	return link, nil
}

func addARPEntry(intfName string, ip net.IP, mac net.HardwareAddr) error {
	logrus.Debugf("vxlan: Adding arp entry ip: %v mac:%v", ip, mac)

	l, err := netlink.LinkByName(intfName)
	if err != nil {
		return err
	}

	n := &netlink.Neigh{
		IP:           ip,
		HardwareAddr: mac,
		LinkIndex:    l.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
	}

	err = netlink.NeighAppend(n)

	if err != nil {
		logrus.Errorf("vxlan: Couldn't add neighbor: %v", err)
		return err
	}

	return nil
}

func delARPEntry(intfName string, ip net.IP, mac net.HardwareAddr) error {
	logrus.Debugf("vxlan: Deleting arp entry ip: %v mac:%v", ip, mac)

	l, err := netlink.LinkByName(intfName)
	if err != nil {
		return err
	}

	n := &netlink.Neigh{
		IP:           ip,
		HardwareAddr: mac,
		LinkIndex:    l.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
	}

	err = netlink.NeighDel(n)

	if err != nil {
		logrus.Errorf("vxlan: Couldn't delete neighbor: %v", err)
		return err
	}

	return nil
}

// `bridge fdb add ${PEER_VTEP_MAC} dev ${VXLAN_INTF} dst ${PEER_HOST_PUBLIC_IP}`
func addVxlanForwardingEntry(intfName string, mac net.HardwareAddr, ip net.IP) error {
	logrus.Debugf("vxlan: Adding route mac: %v ip:%v", mac, ip)

	l, err := netlink.LinkByName(intfName)
	if err != nil {
		return err
	}

	n := &netlink.Neigh{
		IP:           ip,
		HardwareAddr: mac,
		LinkIndex:    l.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		Family:       syscall.AF_BRIDGE,
	}

	err = netlink.NeighAdd(n)

	if err != nil {
		logrus.Errorf("vxlan: Couldn't add neighbor: %v", err)
		return err
	}

	return nil
}

func deleteVxlanForwardingEntry(intfName string, mac net.HardwareAddr, ip net.IP) error {
	logrus.Debugf("vxlan: Deleting route: mac: %v ip:%v", mac, ip)

	l, err := netlink.LinkByName(intfName)
	if err != nil {
		return err
	}

	n := &netlink.Neigh{
		IP:           ip,
		HardwareAddr: mac,
		LinkIndex:    l.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		Family:       syscall.AF_BRIDGE,
	}

	err = netlink.NeighDel(n)

	if err != nil {
		logrus.Errorf("vxlan: Couldn't delete neighbor: %v", err)
		return err
	}

	return nil
}

func addRoute(ip *net.IPNet, via net.IP) error {
	logrus.Debugf("vxlan: adding route: %v via %v", ip, via)
	r := &netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Dst:   ip,
		Gw:    via,
	}

	err := netlink.RouteAdd(r)
	if err != nil {
		logrus.Errorf("vxlan: error adding route: %v", err)
		return err
	}

	return nil
}

func updateRoute(ip *net.IPNet, via net.IP) error {
	logrus.Debugf("vxlan: updating route: %v via %v", ip, via)

	err := delRoute(ip, via)
	if err != nil {
		logrus.Errorf("vxlan: error updating route: %v", err)
		return err
	}

	return addRoute(ip, via)
}

func delRoute(ip *net.IPNet, via net.IP) error {
	logrus.Debugf("vxlan: deleting route: %v via %v", ip, via)
	r := &netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Dst:   ip,
		Gw:    via,
	}

	err := netlink.RouteDel(r)
	if err != nil {
		logrus.Errorf("vxlan: error adding route: %v", err)
		return err
	}

	return nil
}

func calculateDiffOfEntries(oldMap, newMap map[string]store.Entry) entriesDiff {
	logrus.Debugf("newMap:%v", newMap)
	logrus.Debugf("oldMap: %v", oldMap)

	tmpMap := make(map[string]store.Entry)
	for k, v := range oldMap {
		tmpMap[k] = v
	}

	toAdd := make(map[string]store.Entry)
	toUpd := make(map[string]store.Entry)

	for ip, entry := range newMap {
		if oldEntry, ok := tmpMap[ip]; ok {
			if !reflect.DeepEqual(entry, oldEntry) {
				toUpd[ip] = entry
			}
			delete(tmpMap, ip)
		} else {
			toAdd[ip] = entry
		}
	}

	toDel := tmpMap
	logrus.Debugf("toAdd: %v", toAdd)
	logrus.Debugf("toDel: %v", toDel)
	logrus.Debugf("toUpd: %v", toUpd)

	return entriesDiff{toAdd, toDel, toUpd}
}
