package vxlan

import (
	"os/exec"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/vishvananda/netlink"
)

func disableChecksumOffload() error {
	logrus.Infof("disabling tx checksum offload")

	cmdOutput, err := exec.Command("ethtool", "-K", "eth0", "tx", "off").CombinedOutput()
	if err != nil {
		logrus.Errorf("err: %v, cmdOut=%v", err, string(cmdOutput))
		return err
	}

	return nil
}

func getNetLink(intfName string) (netlink.Link, error) {
	link, err := netlink.LinkByName(intfName)
	if err != nil {
		return nil, err
	}
	return link, nil
}

// When environments are linked, the network services across the
// environments are linked. This function goes through the links
// either to/from and figures out the networks of those peers.
func getLinkedPeersInfo(allServices []metadata.Service, selfService metadata.Service, networksMap map[string]metadata.Network, selfNetwork metadata.Network) (map[string]bool, []metadata.Container) {
	linkedPeersNetworks := map[string]bool{}
	var linkedPeersContainers []metadata.Container

	servicesMapByName := getServicesMapByName(allServices, selfService)

	// Find out if the current service has links else if other services link to current service
	if len(selfService.Links) > 0 {
		for linkedServiceName := range selfService.Links {
			linkedServices, ok := servicesMapByName[linkedServiceName]
			logrus.Debugf("linkedServices: %+v", linkedServices)
			if !ok {
				logrus.Errorf("Current service is linked to service: %v, but cannot find in servicesMapByName", linkedServiceName)
				continue
			} else {
				for _, aService := range linkedServices {
					for _, aContainer := range aService.Containers {
						if !(aContainer.State == "running" || aContainer.State == "starting") {
							continue
						}
						// Skip containers whose network names don't match self
						if networksMap[aContainer.NetworkUUID].Name != selfNetwork.Name {
							continue
						}
						linkedPeersContainers = append(linkedPeersContainers, aContainer)
						if _, ok := linkedPeersNetworks[aContainer.NetworkUUID]; !ok {
							linkedPeersNetworks[aContainer.NetworkUUID] = true
						}
					}
				}
			}
		}
	} else {
		linkedFromServices := getLinkedFromServicesToSelf(selfService, allServices, servicesMapByName)
		for _, aService := range linkedFromServices {
			for _, aContainer := range aService.Containers {
				if !(aContainer.State == "running" || aContainer.State == "starting") {
					continue
				}
				// Skip containers whose network names don't match self
				if networksMap[aContainer.NetworkUUID].Name != selfNetwork.Name {
					continue
				}
				linkedPeersContainers = append(linkedPeersContainers, aContainer)
				if _, ok := linkedPeersNetworks[aContainer.NetworkUUID]; !ok {
					linkedPeersNetworks[aContainer.NetworkUUID] = true
				}
			}
		}
	}

	logrus.Debugf("getLinkedPeersInfo linkedPeersNetworks: %+v", linkedPeersNetworks)
	logrus.Debugf("getLinkedPeersInfo linkedPeersContainers: %v", linkedPeersContainers)
	return linkedPeersNetworks, linkedPeersContainers
}

func getLinkedFromServicesToSelf(selfService metadata.Service, allServices []metadata.Service, servicesMapByName map[string][]*metadata.Service) []*metadata.Service {
	linkedTo := selfService.StackName + "/" + selfService.Name
	logrus.Debugf("getLinkedFromServicesToSelf linkedTo: %v", linkedTo)

	var linkedFromServices []*metadata.Service

	for _, service := range allServices {
		if !service.System {
			continue
		}
		linkedFromServiceName := service.StackName + "/" + service.Name
		if len(service.Links) > 0 {
			for linkedService := range service.Links {
				if linkedService != linkedTo {
					continue
				}
				linkedFromServices = append(linkedFromServices, servicesMapByName[linkedFromServiceName]...)
			}
		}
	}

	logrus.Debugf("linkedFromServices: %v", linkedFromServices)
	return linkedFromServices
}

func getServicesMapByName(services []metadata.Service, selfService metadata.Service) map[string][]*metadata.Service {
	// Build serviceMap by "stack_name/service_name"
	// The reason for an array in map value is because of not
	// using UUID but names which can result in duplicates.
	// TODO: Once LinksByUUID is available, use that instead
	servicesMapByName := make(map[string][]*metadata.Service)
	for index, aService := range services {
		if !aService.System || aService.UUID == selfService.UUID {
			continue
		}
		key := aService.StackName + "/" + aService.Name
		if value, ok := servicesMapByName[key]; ok {
			servicesMapByName[key] = append(value, &services[index])

		} else {
			servicesMapByName[key] = []*metadata.Service{&services[index]}
		}
	}
	logrus.Debugf("servicesMapByName: %+v", servicesMapByName)

	return servicesMapByName
}

func getNetworksMap(networks []metadata.Network) map[string]metadata.Network {
	networksMap := map[string]metadata.Network{}

	for _, aNetwork := range networks {
		networksMap[aNetwork.UUID] = aNetwork
	}

	logrus.Debugf("networksMap: %+v", networksMap)
	return networksMap
}

func getHostsMap(hosts []metadata.Host) map[string]metadata.Host {
	hostsMap := map[string]metadata.Host{}

	for _, h := range hosts {
		logrus.Debugf("h: %v", h)
		hostsMap[h.UUID] = h
	}

	logrus.Debugf("hostsMap: %v", hostsMap)
	return hostsMap
}
