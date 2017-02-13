package vxlan

import (
	"fmt"
	"net"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/rancher-net/store"
	"github.com/vishvananda/netlink"
)

type peerVxlanEntry struct {
	intfName string
	ip       net.IP
	ipnet    *net.IPNet
	vtepMAC  net.HardwareAddr
	hostIP   net.IP
}

// newPeerVxlanEntry creates a new struct representing the Peer
// from the given entry
func newPeerVxlanEntry(intfName string, e store.Entry) (*peerVxlanEntry, error) {
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

	vtepMAC, err := getMACAddressForVxlanIP(vxlanMACRange, ip)
	if err != nil {
		logrus.Errorf("Couldn't get MAC address for IP: %v", ip)
		return nil, err
	}
	hostIP := net.ParseIP(e.HostIpAddress)
	if hostIP == nil {
		logrus.Errorf("Couldn't parse host IP address")
		return nil, fmt.Errorf("Couldn't parse host IP address")
	}

	return &peerVxlanEntry{intfName, ip, ipnet, vtepMAC, hostIP}, nil
}

func (v *peerVxlanEntry) add() error {
	var err error

	err = addRoute(v.ipnet, nil, v.intfName)
	if err != nil {
		return err
	}

	err = addARPEntry(v.intfName, v.ip, v.vtepMAC)
	if err != nil {
		return err
	}

	err = addVxlanForwardingEntry(v.intfName, v.vtepMAC, v.hostIP)
	if err != nil {
		delARPEntry(v.intfName, v.ip, v.vtepMAC)
		return err
	}

	return nil
}

func (v *peerVxlanEntry) del() error {
	var err error

	err = delRoute(v.ipnet, nil, v.intfName)
	if err != nil {
		return err
	}

	err = delARPEntry(v.intfName, v.ip, v.vtepMAC)
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
	//var err error

	//err = v.del()
	//if err != nil {
	//	return err
	//}

	//err = v.add()
	//if err != nil {
	//	return err
	//}

	return fmt.Errorf("not updating")
}
