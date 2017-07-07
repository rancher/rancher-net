package vxlan

import (
	"net"
	"syscall"

	"github.com/Sirupsen/logrus"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

func getCurrentFDBEntries(link netlink.Link) (map[string]*netlink.Neigh, error) {
	neighs, err := netlink.NeighList(link.Attrs().Index, syscall.AF_BRIDGE)
	if err != nil {
		logrus.Errorf("Failed to getCurrentFDBEntries, NeighList: %v", err)
		return nil, err
	}

	fdbEntries := make(map[string]*netlink.Neigh)
	for index, n := range neighs {
		logrus.Debugf("getCurrentFDBEntries: Neigh %+v", n)
		fdbEntries[n.IP.To4().String()] = &neighs[index]
	}

	logrus.Debugf("getCurrentFDBEntries: fdbEntries %v", fdbEntries)
	return fdbEntries, nil
}

func getDesiredFDBEntries(link netlink.Link, fdb map[string]net.HardwareAddr) map[string]*netlink.Neigh {
	fdbEntries := make(map[string]*netlink.Neigh)

	for ip, mac := range fdb {
		n := &netlink.Neigh{
			IP:           net.ParseIP(ip),
			HardwareAddr: mac,
			LinkIndex:    link.Attrs().Index,
			State:        netlink.NUD_PERMANENT,
			Flags:        netlink.NTF_SELF,
			Family:       syscall.AF_BRIDGE,
		}
		fdbEntries[ip] = n
	}
	logrus.Debugf("getDesiredFDBEntries: fdbEntries %v", fdbEntries)
	return fdbEntries
}

func updateFDB(oldEntries map[string]*netlink.Neigh, newEntries map[string]*netlink.Neigh) error {
	var e error

	for ip, oe := range oldEntries {
		ne, ok := newEntries[ip]
		if ok {
			if ne.HardwareAddr.String() != oe.HardwareAddr.String() {
				err := netlink.NeighDel(oe)
				if err != nil {
					logrus.Errorf("updateFDB: failed to NeighDel, %v", err)
					e = errors.Wrap(e, err.Error())
				}
			} else {
				delete(newEntries, ip)
			}
		} else {
			err := netlink.NeighDel(oe)
			if err != nil {
				logrus.Errorf("updateFDB: failed to NeighDel not in newEntries, %v", err)
				e = errors.Wrap(e, err.Error())
			}
		}
	}

	for ip, ne := range newEntries {
		err := netlink.NeighAppend(ne)
		if err != nil {
			logrus.Errorf("updateFDB: failed to NeighAppend, %v, %s", err, ip)
			e = errors.Wrap(e, err.Error())
		}
	}

	return e
}
