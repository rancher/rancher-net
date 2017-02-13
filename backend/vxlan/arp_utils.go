package vxlan

import (
	"github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"net"
)

func addARPEntry(intfName string, ip net.IP, mac net.HardwareAddr) error {
	logrus.Debugf("vxlan: Adding arp entry ip: %v mac:%v inftName: %v", ip, mac, intfName)

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
	logrus.Debugf("vxlan: Deleting arp entry ip: %v mac:%v intfName: %v", ip, mac, intfName)

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
