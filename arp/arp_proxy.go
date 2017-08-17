package arp

import (
	"bytes"
	"net"

	"github.com/Sirupsen/logrus"
	"github.com/mdlayher/arp"
	"github.com/mdlayher/ethernet"
	"github.com/rancher/rancher-net/backend/vxlan"
	"github.com/rancher/rancher-net/store"
)

// ListenAndServe starts ARP proxy server
func ListenAndServe(db store.Store, ifaceName string) error {
	listenIface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return err
	}

	client, err := arp.NewClient(listenIface)
	if err != nil {
		return err
	}

	logrus.Infof("Listening for ARP requests on %s", ifaceName)
	for {
		arpRequest, iface, err := client.Read()
		if err != nil {
			return err
		}

		if arpRequest.Operation != arp.OperationRequest ||
			(!bytes.Equal(iface.Destination, ethernet.Broadcast) &&
				!bytes.Equal(iface.Destination, listenIface.HardwareAddr)) {
			continue
		}

		targetIP := arpRequest.TargetIP.String()
		logrus.Debugf("Arp request for %s", targetIP)
		if db.IsRemote(targetIP) {
			logrus.Debugf("Sending arp reply for %s", targetIP)
			if err := client.Reply(arpRequest, listenIface.HardwareAddr, arpRequest.TargetIP); err != nil {
				return err
			}
		}
	}
}

// ListenAndServeForVXLAN starts ARP proxy server for VXLAN
func ListenAndServeForVXLAN(o *vxlan.Overlay, ifaceName string) error {
	listenIface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return err
	}

	client, err := arp.NewClient(listenIface)
	if err != nil {
		return err
	}

	logrus.Infof("Listening for ARP requests on %s", ifaceName)
	for {
		arpRequest, iface, err := client.Read()
		if err != nil {
			return err
		}

		if arpRequest.Operation != arp.OperationRequest ||
			(!bytes.Equal(iface.Destination, ethernet.Broadcast) &&
				!bytes.Equal(iface.Destination, listenIface.HardwareAddr)) {
			continue
		}

		targetIP := arpRequest.TargetIP.String()
		logrus.Debugf("Arp request for %s", targetIP)
		if o.IsRemote(targetIP) {
			logrus.Debugf("Sending arp reply for %s", targetIP)
			client.Reply(arpRequest, listenIface.HardwareAddr, arpRequest.TargetIP)
		}
	}
}
