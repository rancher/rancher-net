package vxlan

import (
	"os/exec"
	"reflect"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/rancher-net/store"
)

func calculateDiffOfEntries(oldMap, newMap map[string]store.Entry) entriesDiff {
	logrus.Debugf("calculateDiffOfEntries")
	logrus.Debugf("newMap:%v", newMap)
	logrus.Debugf("oldMap: %v", oldMap)

	tmpMap := make(map[string]store.Entry)
	for k, v := range oldMap {
		tmpMap[k] = v
	}

	toAdd := make(map[string]store.Entry)
	toUpd := make(map[string]store.Entry)
	noop := make(map[string]store.Entry)

	for ip, entry := range newMap {
		if oldEntry, ok := tmpMap[ip]; ok {
			if !reflect.DeepEqual(entry, oldEntry) {
				toUpd[ip] = entry
			} else {
				noop[ip] = entry
			}
			delete(tmpMap, ip)
		} else {
			toAdd[ip] = entry
		}
	}

	toDel := tmpMap
	logrus.Debugf("toAdd: %v", toAdd)
	logrus.Debugf("toDel: %v", toDel)
	logrus.Debugf("toUpd: %v", toUpd)
	logrus.Debugf("noop: %v", noop)

	return entriesDiff{toAdd, toDel, toUpd, noop}
}

func disableChecksumOffload() error {
	logrus.Infof("disabling tx checksum offload")

	cmdOutput, err := exec.Command("ethtool", "-K", "eth0", "tx", "off").CombinedOutput()
	if err != nil {
		logrus.Errorf("err: %v, cmdOut=%v", err, string(cmdOutput))
		return err
	}

	return nil
}
