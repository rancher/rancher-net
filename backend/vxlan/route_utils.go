package vxlan

import (
	"net"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

func getCurrentRouteEntries(link netlink.Link) (map[string]*netlink.Route, error) {
	existRoutes, err := netlink.RouteList(link, netlink.FAMILY_V4)
	if err != nil {
		logrus.Errorf("Failed to getCurrentRouteEntries, RouteList: %v", err)
		return nil, err
	}

	routeEntries := make(map[string]*netlink.Route)
	for index, r := range existRoutes {
		dstIP := strings.Split(r.Dst.IP.To4().String(), "/")[0]
		routeEntries[dstIP] = &existRoutes[index]
	}

	logrus.Debugf("getCurrentRouteEntries: routeEntries %v", routeEntries)
	return routeEntries, nil
}

func getDesiredRouteEntries(link netlink.Link, routes map[string]*net.IPNet) map[string]*netlink.Route {
	routeEntries := make(map[string]*netlink.Route)

	for ip, ipnet := range routes {
		r := &netlink.Route{
			Scope:     netlink.SCOPE_UNIVERSE,
			Dst:       ipnet,
			LinkIndex: link.Attrs().Index,
		}
		routeEntries[ip] = r
	}

	logrus.Debugf("getDesiredRouteEntries: routeEntries %v", routeEntries)
	return routeEntries
}

func updateRoute(oldEntries map[string]*netlink.Route, newEntries map[string]*netlink.Route) error {
	var e error

	for ip, oe := range oldEntries {
		_, ok := newEntries[ip]
		if ok {
			delete(newEntries, ip)
		} else {
			err := netlink.RouteDel(oe)
			if err != nil {
				logrus.Errorf("updateRoute: failed to RouteDel, %v", err)
				e = errors.Wrap(e, err.Error())
			}
		}
	}

	for _, ne := range newEntries {
		err := netlink.RouteAdd(ne)
		if err != nil {
			logrus.Errorf("updateRoute: failed to RouteAdd, %v", err)
			e = errors.Wrap(e, err.Error())
		}
	}

	return e
}
