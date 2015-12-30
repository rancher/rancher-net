package arp

import (
	"bytes"
	"net"

	"github.com/Sirupsen/logrus"
	"github.com/mdlayher/arp"
	"github.com/mdlayher/ethernet"
	"github.com/rancher/rancher-net/store"
)

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

		targetIp := arpRequest.TargetIP.String()
		logrus.Debugf("Arp request for %s", targetIp)
		if db.IsRemote(targetIp) {
			logrus.Debugf("Sending arp reply for %s", targetIp)
			if err := client.Reply(arpRequest, listenIface.HardwareAddr, arpRequest.TargetIP); err != nil {
				return err
			}
		}
	}
}
