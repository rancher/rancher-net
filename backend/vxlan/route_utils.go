package vxlan

import (
	"net"

	"github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

func addRoute(ip *net.IPNet, via net.IP, intfName string) error {
	logrus.Debugf("vxlan: adding route: %v via %v intfName: %v", ip, via, intfName)
	r := &netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Dst:   ip,
	}

	if via != nil {
		r.Gw = via
	}

	if intfName != "" {
		l, err := findVxlanInterface(intfName)
		if err != nil {
			logrus.Errorf("Couldn't find link by name: %v", intfName)
			return err
		}
		r.LinkIndex = l.Attrs().Index
	}

	err := netlink.RouteAdd(r)
	if err != nil {
		logrus.Errorf("vxlan: error adding route: %v", err)
		return err
	}

	return nil
}

func updateRoute(ip *net.IPNet, via net.IP, intfName string) error {
	logrus.Debugf("vxlan: updating route: %v via %v intfName: %v", ip, via, intfName)

	err := delRoute(ip, nil, intfName)
	if err != nil {
		logrus.Errorf("vxlan: error updating route: %v", err)
		return err
	}

	return addRoute(ip, via, intfName)
}

func delRoute(ip *net.IPNet, via net.IP, intfName string) error {
	logrus.Debugf("vxlan: deleting route: %v via %v intfName: %v", ip, via, intfName)
	r := &netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Dst:   ip,
	}

	if via != nil {
		r.Gw = via
	}

	if intfName != "" {
		l, err := findVxlanInterface(intfName)
		if err != nil {
			logrus.Errorf("Couldn't find link by name: %v", intfName)
			return err
		}
		r.LinkIndex = l.Attrs().Index
	}

	err := netlink.RouteDel(r)
	if err != nil {
		logrus.Errorf("vxlan: error adding route: %v", err)
		return err
	}

	return nil
}
