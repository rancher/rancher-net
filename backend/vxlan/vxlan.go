// Package vxlan provides the capabilities to create a VXLAN overlay network
package vxlan

import (
	"errors"
	"net"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/rancher-net/store"
)

const (
	metadataURL = "http://rancher-metadata/2015-12-19"

	vxlanInterfaceName = "vtep1042"
	vxlanVni           = 1042
	vxlanMACRange      = "0E:00:00:00:00:00"
	vxlanMTU           = 1400
	vxlanPort          = 4789
	//vxlanPort          = 46354 //There is a bug in netlink library 46354 ~ swapped 4789

	emptyIPAddress = ""
)

// Overlay is used to store the VXLAN overlay information
type Overlay struct {
	sync.Mutex

	m                        metadata.Client
	db                       store.Store
	peersMapping             map[string]net.IP
	prevPeerEntries          map[string]store.Entry
	prevNonPeerRemoteEntries map[string]store.Entry
	v                        *vxlanIntfInfo
}

type entriesDiff struct {
	toAdd map[string]store.Entry
	toDel map[string]store.Entry
	toUpd map[string]store.Entry
	noop  map[string]store.Entry
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

	var firstErr error
	//localHostIP := o.db.LocalHostIpAddress()

	// Install/Update/Delete peer entries
	currentPeerEntries := o.db.PeerEntriesMap()
	delete(currentPeerEntries, o.db.LocalIpAddress())
	peerEntriesDiff := calculateDiffOfEntries(o.prevPeerEntries, currentPeerEntries)
	o.handlePeerEntries(peerEntriesDiff)

	o.peersMapping = buildPeersMapping(o.prevPeerEntries)

	// Install/Update/Delete remote entries
	curNonPeerRemoteEntries := o.db.RemoteNonPeerEntriesMap()
	remoteEntriesDiff := calculateDiffOfEntries(o.prevNonPeerRemoteEntries, curNonPeerRemoteEntries)
	o.handleNonPeerRemoteEntries(remoteEntriesDiff, o.peersMapping)

	return firstErr
}

func (o *Overlay) cleanup() error {
	//o.Lock()
	//defer o.Unlock()
	logrus.Infof("vxlan: cleanup")

	curNonPeerRemoteEntries := o.db.RemoteNonPeerEntriesMap()
	cleanupRemoteEntriesDiff := entriesDiff{
		map[string]store.Entry{},
		curNonPeerRemoteEntries,
		map[string]store.Entry{},
		map[string]store.Entry{},
	}
	o.handleNonPeerRemoteEntries(cleanupRemoteEntriesDiff, o.peersMapping)

	currentPeerEntries := o.db.PeerEntriesMap()
	cleanupPeerEntriesDiff := entriesDiff{
		map[string]store.Entry{},
		currentPeerEntries,
		map[string]store.Entry{},
		map[string]store.Entry{},
	}
	o.handlePeerEntries(cleanupPeerEntriesDiff)

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
			peersMapping[entry.HostIpAddress] = entryIP
		}
	}

	logrus.Debugf("peersMapping: %+v", peersMapping)
	return peersMapping
}

// handlePeerEntries takes care of installing the ARP and bridge entry
// Peer = Container running the rancher-net/ipsec/vxlan
func (o *Overlay) handlePeerEntries(diff entriesDiff) {
	logrus.Debugf("before handlePeerEntries o.prevPeerEntries: %+v", o.prevPeerEntries)

	prevPeerEntries := o.prevPeerEntries
	o.prevPeerEntries = make(map[string]store.Entry)

	for _, e := range diff.noop {
		ipNoCidr := strings.Split(e.IpAddress, "/")[0]
		o.prevPeerEntries[ipNoCidr] = e
	}

	logrus.Debugf("handlePeerEntries: toAdd: %+v", diff.toAdd)
	for _, e := range diff.toAdd {
		logrus.Debugf("e: %v", e)
		if e.Self {
			logrus.Debugf("skipping self")
			continue
		}

		peer, err := newPeerVxlanEntry(o.v.name, e)
		if err != nil {
			logrus.Errorf("Error creating new peer entry: %v", err)
			continue
		}
		if peer == nil {
			logrus.Errorf("Got nil for e: %v", e)
			continue
		}
		logrus.Debugf("vxlan: Adding peer: %+v", *peer)
		err = peer.add()
		if err != nil {
			logrus.Errorf("vxlan: error adding peer entry: %v", err)
		} else {
			ipNoCidr := strings.Split(e.IpAddress, "/")[0]
			o.prevPeerEntries[ipNoCidr] = e
		}
	}

	logrus.Debugf("handlePeerEntries: toDel: %+v", diff.toDel)
	for _, e := range diff.toDel {
		logrus.Debugf("e: %v", e)
		if e.Self {
			logrus.Debugf("skipping self")
			continue
		}

		peer, err := newPeerVxlanEntry(o.v.name, e)
		if err != nil {
			logrus.Errorf("Error creating new peer entry: %v", err)
			continue
		}
		if peer == nil {
			logrus.Errorf("Got nil for e: %v", e)
			continue
		}
		logrus.Debugf("vxlan: Deleting peer: %+v", *peer)
		err = peer.del()
		if err != nil {
			logrus.Errorf("vxlan: error deleting peer entry: %v", err)
			ipNoCidr := strings.Split(e.IpAddress, "/")[0]
			o.prevPeerEntries[ipNoCidr] = e
		}
	}

	logrus.Debugf("handlePeerEntries: toUpd: %+v", diff.toUpd)
	for _, e := range diff.toUpd {
		logrus.Debugf("e: %v", e)
		ipNoCidr := strings.Split(e.IpAddress, "/")[0]
		if e.Self {
			logrus.Debugf("skipping self")
			continue
		}

		logrus.Debugf("Not processing update of e: %+v", e)
		o.prevPeerEntries[ipNoCidr] = prevPeerEntries[ipNoCidr]
		continue

		peer, err := newPeerVxlanEntry(o.v.name, e)
		if err != nil {
			logrus.Errorf("Error creating new peer entry: %v, keep prev entry", err)
			o.prevPeerEntries[ipNoCidr] = prevPeerEntries[ipNoCidr]
			continue
		}
		if peer == nil {
			logrus.Errorf("Got nil for e: %v, keep prev entry", e)
			o.prevPeerEntries[ipNoCidr] = prevPeerEntries[ipNoCidr]
			continue
		}
		logrus.Debugf("vxlan: Updating peer: %+v", *peer)
		err = peer.upd()
		if err != nil {
			logrus.Errorf("vxlan: error adding peer entry: %v, keep prev entry", err)
			// If there was an error updating, keep the old entry
			o.prevPeerEntries[ipNoCidr] = prevPeerEntries[ipNoCidr]
		} else {
			o.prevPeerEntries[ipNoCidr] = e
		}
	}

	logrus.Debugf("after handlePeerEntries: o.prevPeerEntries: %+v", o.prevPeerEntries)
}

func (o *Overlay) handleNonPeerRemoteEntries(diff entriesDiff, peersMapping map[string]net.IP) {
	logrus.Debugf("before handleNonPeerRemoteEntries prevNonPeerRemoteEntries: %+v", o.prevNonPeerRemoteEntries)

	prevNonPeerRemoteEntries := o.prevNonPeerRemoteEntries
	o.prevNonPeerRemoteEntries = make(map[string]store.Entry)

	for _, e := range diff.noop {
		ipNoCidr := strings.Split(e.IpAddress, "/")[0]
		o.prevNonPeerRemoteEntries[ipNoCidr] = e
	}

	logrus.Debugf("handleNonPeerRemoteEntries: toAdd: %+v", diff.toAdd)
	for _, e := range diff.toAdd {
		logrus.Debugf("e: %v", e)
		rEntry, err := newRemoteVxlanEntry(o.v.name, e, peersMapping)
		if err != nil {
			logrus.Errorf("Error creating new remote entry: %v", err)
			continue
		}
		if rEntry == nil {
			logrus.Debugf("Got nil for e: %v", e)
			continue
		}
		if rEntry.via == nil {
			logrus.Debugf("Got via=nil for e: %v", e)
			continue
		}

		logrus.Debugf("vxlan: Adding remote entry: %+v", *rEntry)
		err = rEntry.add()
		if err != nil {
			logrus.Errorf("vxlan: error adding remote entry: %v", err)
		} else {
			ipNoCidr := strings.Split(e.IpAddress, "/")[0]
			o.prevNonPeerRemoteEntries[ipNoCidr] = e
		}
	}

	logrus.Debugf("handleNonPeerRemoteEntries: toDel: %v", diff.toDel)
	for _, e := range diff.toDel {
		logrus.Debugf("e: %v", e)
		rEntry, err := newRemoteVxlanEntry(o.v.name, e, peersMapping)
		if err != nil {
			logrus.Errorf("Error creating new remote entry: %v", err)
			continue
		}
		if rEntry == nil {
			logrus.Errorf("Got nil for e: %v", e)
			continue
		}

		logrus.Debugf("vxlan: Deleting remote entry: %+v", *rEntry)
		err = rEntry.del()
		if err != nil {
			logrus.Errorf("vxlan: error deleting remote entry: %v", err)
			ipNoCidr := strings.Split(e.IpAddress, "/")[0]
			o.prevNonPeerRemoteEntries[ipNoCidr] = e
		}
	}

	logrus.Debugf("handleNonPeerRemoteEntries: toUpd: %v", diff.toUpd)
	for _, e := range diff.toUpd {
		logrus.Debugf("e: %v", e)
		ipNoCidr := strings.Split(e.IpAddress, "/")[0]

		logrus.Debugf("Not processing update of e: %+v", e)
		o.prevNonPeerRemoteEntries[ipNoCidr] = prevNonPeerRemoteEntries[ipNoCidr]
		continue

		rEntry, err := newRemoteVxlanEntry(o.v.name, e, peersMapping)
		if err != nil {
			logrus.Errorf("Error creating new remote entry: %v", err)
			o.prevNonPeerRemoteEntries[ipNoCidr] = prevNonPeerRemoteEntries[ipNoCidr]
			continue
		}
		if rEntry == nil {
			logrus.Errorf("Got nil for e: %v", e)
			o.prevNonPeerRemoteEntries[ipNoCidr] = prevNonPeerRemoteEntries[ipNoCidr]
			continue
		}

		logrus.Debugf("vxlan: Updating remote entry: %+v", *rEntry)
		err = rEntry.upd()
		if err != nil {
			logrus.Errorf("vxlan: error updating remote entry: %v", err)
			// If there was an error updating, keep the old entry
			o.prevNonPeerRemoteEntries[ipNoCidr] = prevNonPeerRemoteEntries[ipNoCidr]
		} else {
			o.prevNonPeerRemoteEntries[ipNoCidr] = e
		}
	}

	logrus.Debugf("handleNonPeerRemoteEntries: o.prevNonPeerRemoteEntries: %+v", o.prevNonPeerRemoteEntries)
}

// GetMyVTEPInfo is used to figure out the MAC address to be assigned
// for the VTEP address.
func (o *Overlay) GetMyVTEPInfo() (net.HardwareAddr, error) {
	logrus.Debugf("vxlan: GetMyVTEPInfo")

	myRancherIPString := o.db.LocalIpAddress()
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
