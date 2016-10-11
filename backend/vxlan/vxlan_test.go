package vxlan

import (
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/rancher/rancher-net/store"
	"github.com/vishvananda/netlink"
	"math/rand"
	"net"
	"os"
	"testing"
	"time"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func getRandomVxlanInterface() *vxlanIntfInfo {
	vni := 10000 + rand.Intn(50000)
	name := fmt.Sprintf("vtep%v", vni)

	vtepIPStr1 := "169.254.1.1"
	vtepIP1 := net.ParseIP(vtepIPStr1)
	vtepMAC1, _ := net.ParseMAC("00:00:00:00:01:01")

	vx := &vxlanIntfInfo{
		name: name,
		vni:  vni,
		port: vni,
		ip:   vtepIP1,
		mac:  vtepMAC1,
		mtu:  vxlanMTU,
	}
	logrus.Debugf("vx: %+v", vx)
	return vx
}

func TestABCD(t *testing.T) {
	getRandomVxlanInterface()
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Some of the tests which involve the actual interfaces, need to
// run either in a privileged container or using sudo.
// Easy way would be to mount the source code inside a go container
// and build/run/test.

func TestGetIPAddressInVxlanSubnetRange(t *testing.T) {
	_, err := getIPAddressInVxlanSubnet(allowedStartIndex - 1)
	if err == nil {
		t.Error("expected an error, got nil")
	}

	_, err = getIPAddressInVxlanSubnet(allowedEndIndex + 1)
	if err == nil {
		t.Error("expected an error, got nil")
	}
}

func TestGetIPAddressInVxlanSubnet(t *testing.T) {
	ip, err := getIPAddressInVxlanSubnet(2)
	if err != nil {
		t.Error("Not expecting an error")
	} else {
		expected := "169.254.0.2"
		if ip.String() != expected {
			t.Error("Invalid IP address returned, expected: %v, got: %v", expected, ip)
		}
	}

	ip, err = getIPAddressInVxlanSubnet(256)
	if err != nil {
		t.Error("Not expecting an error")
	} else {
		expected := "169.254.1.0"
		if ip.String() != expected {
			t.Error("Invalid IP address returned, expected: %v, got: %v", expected, ip)
		}
	}
}

func TestGetIPAddressInVxlanSubnetForMetadataIndex(t *testing.T) {
	ip, err := getIPAddressInVxlanSubnet(blackListIndex)
	if err != nil {
		t.Error("Not expecting an error")
	} else {
		expected := "169.254.255.254"
		if ip.String() != expected {
			t.Error("Invalid IP address returned, expected: %v, got: %v", expected, ip)
		}
	}
}

func TestCreateDeleteVTEP(t *testing.T) {
	vx := getRandomVxlanInterface()
	o, _ := NewTestOverlay(vx, nil)

	err := o.checkAndCreateVTEP()
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = o.checkAndDeleteVTEP()
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}
}

func TestAddDeleteVxlanStaticRoute(t *testing.T) {
	vx := getRandomVxlanInterface()
	o, _ := NewTestOverlay(vx, nil)

	ip := net.ParseIP("1.1.1.1")
	mac, _ := net.ParseMAC("00:00:11:11:11:11")

	err := o.checkAndCreateVTEP()
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = addVxlanForwardingEntry(vx.name, mac, ip)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = deleteVxlanForwardingEntry(vx.name, mac, ip)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = o.checkAndDeleteVTEP()
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

}

func TestGetMACAddressForVxlanIP(t *testing.T) {
	inputVxlanIP := "169.254.29.78"
	expected := "00:ab:a9:fe:1d:4e"

	actual, _ := getMACAddressForVxlanIP(net.ParseIP(inputVxlanIP))
	if actual.String() != expected {
		t.Error("expected: %v, actual: %v", expected, actual)
	}
}

func TestMapRancherIPToVxlanIP(t *testing.T) {
	rancherIPStr := "10.42.29.78"
	expectedVxlanIPStr := "169.254.29.78"

	actualIP, _ := mapRancherIPToVxlanIP(net.ParseIP(rancherIPStr))
	if actualIP.String() != expectedVxlanIPStr {
		t.Error("Expected: %v actual: %v", expectedVxlanIPStr, actualIP)
	}
}

func TestAddDelRoute(t *testing.T) {
	vtepIPStr1 := "169.254.1.1"
	vtepIPStr2 := "169.254.2.2"
	//vtepIPStr3 := "169.254.3.3"
	vtepIP1 := net.ParseIP(vtepIPStr1)
	vtepMAC1, _ := net.ParseMAC("00:00:00:00:01:01")
	intfName := randSeq(8)

	v := &vxlanIntfInfo{
		name: intfName,
		vni:  vxlanVni,
		port: vxlanPort,
		ip:   vtepIP1,
		mac:  vtepMAC1,
		mtu:  vxlanMTU,
	}

	err := createVxlanInterface(v)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	ip, _ := netlink.ParseIPNet("10.1.1.1/32")

	via1 := net.ParseIP(vtepIPStr2)
	//via2 := net.ParseIP(vtepIPStr3)

	err = addRoute(ip, via1)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = delRoute(ip, via1)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = deleteVxlanInterface(intfName)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

}

func TestAddDelARPEntry(t *testing.T) {
	vx := getRandomVxlanInterface()
	o, _ := NewTestOverlay(vx, nil)

	o.checkAndCreateVTEP()

	ip := net.ParseIP("1.1.1.1")
	mac, _ := net.ParseMAC("00:00:11:11:11:11")

	err := addARPEntry(vx.name, ip, mac)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = delARPEntry(vx.name, ip, mac)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	o.checkAndDeleteVTEP()
}

func waitForFile(file string) string {
	for i := 0; i < 60; i++ {
		if _, err := os.Stat(file); err == nil {
			return file
		}

		logrus.Infof("Waiting for file %s", file)
		time.Sleep(1 * time.Second)
	}
	logrus.Fatalf("Failed to find %s", file)
	return ""
}

func TestDiffEntries(t *testing.T) {
	oldDB := store.NewSimpleStore(waitForFile("oldEntries.json"), "")
	oldDB.Reload()

	newDB := store.NewSimpleStore(waitForFile("newEntries.json"), "")
	newDB.Reload()

	diff := calculateDiffOfEntries(oldDB.RemoteEntriesMap(), newDB.RemoteEntriesMap())

	if len(diff.toAdd) != 1 || len(diff.toDel) != 1 || len(diff.toUpd) != 1 {
		t.Errorf("not execpected lengths")
	}
}

func TestDBEntries(t *testing.T) {
	db := store.NewSimpleStore(waitForFile("entries.json"), "")
	db.Reload()

	m := buildPeersMapping(db.PeerEntriesMap())

	expectedLength := 1
	actualLength := len(m)
	if actualLength != expectedLength {
		t.Errorf("expected length: %v got: %v", expectedLength, actualLength)
	}

	expectedIP := "169.254.100.17"
	actualIP := m["172.22.101.101"].String()
	if actualIP != expectedIP {
		t.Errorf("expected: %v got: %v", expectedIP, actualIP)
	}
}

func TestFindVxlanInterace(t *testing.T) {
	_, err := findVxlanInterface("vtep123")
	if err == nil {
		t.Errorf("Expecting error, but got no error")
	}

	_, err = findVxlanInterface("eth0")
	if err != nil {
		t.Errorf("Not expecting error, but got %v", err)
	}
}

func TestPeerVxlanEntryOperations(t *testing.T) {
	var err error

	vtepIPStr1 := "169.254.1.1"
	vtepIP1 := net.ParseIP(vtepIPStr1)
	vtepMAC1, _ := net.ParseMAC("00:00:00:00:01:01")
	intfName := randSeq(8)

	vx := &vxlanIntfInfo{
		name: intfName,
		vni:  vxlanVni,
		port: vxlanPort,
		ip:   vtepIP1,
		mac:  vtepMAC1,
		mtu:  vxlanMTU,
	}

	err = createVxlanInterface(vx)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}
	e := store.Entry{
		IpAddress:     "10.42.1.1/16",
		HostIpAddress: "52.1.1.1",
	}

	v := newPeerVxlanEntry(intfName, e)
	if v == nil {
		t.Errorf("Not expecting nil")
	}

	err = v.add()
	if err != nil {
		t.Errorf("Not expecting error, got: %v", err)
	}

	err = v.del()
	if err != nil {
		t.Errorf("Not expecting error, got: %v", err)
	}

	err = deleteVxlanInterface(intfName)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

}

func TestRemoteVxlanEntryOperations(t *testing.T) {
	var err error

	vx := getRandomVxlanInterface()
	err = createVxlanInterface(vx)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}
	db := store.NewSimpleStore(waitForFile("entries.json"), "")
	db.Reload()

	m := buildPeersMapping(db.PeerEntriesMap())

	e := store.Entry{
		IpAddress:     "10.42.13.108/16",
		HostIpAddress: "172.22.101.101",
	}

	v := newRemoteVxlanEntry(e, m)
	if v == nil {
		t.Errorf("Not expecting nil")
	}

	expectedVia := "169.254.100.17"
	actualVia := v.via.String()
	if actualVia != expectedVia {
		t.Errorf("expected: %v actual: %v", expectedVia, actualVia)
	}

	err = v.add()
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = v.del()
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}

	err = deleteVxlanInterface(vx.name)
	if err != nil {
		t.Error("No error expected, got: %v", err)
	}
}

func TestVxlanFunctionality(t *testing.T) {
	db, _ := store.NewMetadataStore("")
	logrus.Infof("db: %+v", db)
	db.Reload()

	o, _ := NewOverlay("", db)
	logrus.Infof("o=%+v", o)
	o.configure()
	o.Reload()
	o.cleanup()
}

func TestGetMyRancherIPFromInterface(t *testing.T) {
	ip, err := getMyRancherIPFromInterface()
	if err != nil {
		t.Error("not expecting error, got: %v", err)
	}

	logrus.Debugf("ip: %v", ip)
}

func TestGetMyVtepInfo(t *testing.T) {
	vx := getRandomVxlanInterface()
	db, _ := store.NewMetadataStore("")
	db.Reload()
	o, _ := NewTestOverlay(vx, db)

	ip, mac, _ := o.GetMyVTEPInfo()

	if ip.String() == "" || mac.String() == "" {
		t.Error("Expecting an ip address")
	}
}

/*
func TestPrintRange(t *testing.T) {
	for i := allowedStartIndex; i <= allowedEndIndex; i++ {
		ip, err := getIPAddressInVxlanSubnet(i)
		if err != nil {
			t.Error("Not expecting an error")
		} else {
			logrus.Debugf("%v : %v", ip, i)
		}
	}
}
*/
