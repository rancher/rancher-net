package vxlan

import (
	"fmt"
	"net"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/rancher-net/store"
	"github.com/vishvananda/netlink"
)

type remoteVxlanEntry struct {
	intfName string
	ip       net.IP
	ipnet    *net.IPNet
	via      net.IP
	vtepMAC  net.HardwareAddr
}

// newRemoteVxlanEntry create a new struct representing the
// non peer remote entry
// Need to change /16 to /32, else the route is not getting installed
func newRemoteVxlanEntry(intfName string, e store.Entry, peersMapping map[string]net.IP) (*remoteVxlanEntry, error) {
	var err error
	logrus.Debugf("building remoteVxlanEntry for %v", e)

	ip, _, err := net.ParseCIDR(e.IpAddress)
	if err != nil {
		logrus.Errorf("Couldn't parseCIDR for IP: %v", e.IpAddress)
		return nil, err
	}

	ipslash32 := strings.Split(e.IpAddress, "/")[0] + "/32"

	ipnet, err := netlink.ParseIPNet(ipslash32)
	if err != nil {
		logrus.Errorf("Couldn't ParseIPNet for IP: %v", e.IpAddress)
		return nil, err
	}

	var via net.IP
	if e.HostIpAddress != "" {
		via = peersMapping[e.HostIpAddress]
	} else {
		via = nil
	}

	var vtepMAC net.HardwareAddr
	if via != nil {
		vtepMAC, err = getMACAddressForVxlanIP(vxlanMACRange, via)
		if err != nil {
			logrus.Errorf("Couldn't get MAC address for IP: %v", via)
			return nil, err
		}
	} else {
		vtepMAC = nil
	}
	return &remoteVxlanEntry{intfName, ip, ipnet, via, vtepMAC}, nil
}

func (v *remoteVxlanEntry) add() error {
	err := addRoute(v.ipnet, nil, v.intfName)
	if err != nil {
		return err
	}

	err = addARPEntry(v.intfName, v.ip, v.vtepMAC)
	if err != nil {
		return err
	}

	return nil
}

func (v *remoteVxlanEntry) del() error {
	err := delRoute(v.ipnet, nil, v.intfName)
	if err != nil {
		return err
	}

	err = delARPEntry(v.intfName, v.ip, v.vtepMAC)
	if err != nil {
		return err
	}

	return nil
}

func (v *remoteVxlanEntry) upd() error {
	//err := updateRoute(v.ipnet, nil, v.intfName)
	//if err != nil {
	//	return err
	//}

	//err = delARPEntry(v.intfName, v.ip, v.vtepMAC)
	//if err != nil {
	//	return err
	//}

	//err = addARPEntry(v.intfName, v.ip, v.vtepMAC)
	//if err != nil {
	//	return err
	//}

	return fmt.Errorf("not updating")
}
