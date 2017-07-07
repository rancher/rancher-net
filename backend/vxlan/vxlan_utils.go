package vxlan

import (
	"net"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	linkUpRetries = 3
)

type vxlanIntfInfo struct {
	name string
	vni  int
	port int
	mac  net.HardwareAddr
	mtu  int
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

	linkUpSuccessful := false
	retries := linkUpRetries
	for retries > 0 {
		err = netlink.LinkSetUp(vxlan)
		if err != nil {
			logrus.Debugf("setting link up got error: %v", err)
		} else {
			linkUpSuccessful = true
			break
		}
		time.Sleep(200 * time.Millisecond)
		retries--
	}

	if !linkUpSuccessful {
		logrus.Errorf("Couldn't set link up got error: %v", err)
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

// getMACAddressForVxlanIP uses the input VXLAN MAC prefix, IP
// to build a MAC address.
// For example if the MAC prefix is 0E:00:00:00:00:00 and
// the IP address is 1.2.3.4, the MAC returned is 0E:00:01:02:03:04
func getMACAddressForVxlanIP(prefix string, ip net.IP) (net.HardwareAddr, error) {
	mac, err := net.ParseMAC(prefix)
	if err != nil {
		return nil, err
	}

	ip4 := ip.To4()
	mac[5] = ip4[3]
	mac[4] = ip4[2]
	mac[3] = ip4[1]
	mac[2] = ip4[0]

	return mac, nil
}
