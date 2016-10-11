package store

import (
	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	rmd "github.com/rancher/rancher-metadata"
	"reflect"
	"testing"
)

const (
	mdVersion = "2015-12-19"

	listenPort1       = ":30001"
	listenReloadPort1 = ":30011"
	metadataURL1      = "http://localhost" + listenPort1 + "/" + mdVersion
	answers1          = "./metadata_test_data/answers.host1.yml"

	listenPort2       = ":30002"
	listenReloadPort2 = ":30012"
	metadataURL2      = "http://localhost" + listenPort2 + "/" + mdVersion
	answers2          = "./metadata_test_data/answers.host2.yml"

	listenPort3       = ":30003"
	listenReloadPort3 = ":30013"
	metadataURL3      = "http://localhost" + listenPort3 + "/" + mdVersion
	answers3          = "./metadata_test_data/answers.host3.yml"

	simpleFile1 = "./metadata_test_data/ipsec.host1.json"
	simpleFile2 = "./metadata_test_data/ipsec.host2.json"
	simpleFile3 = "./metadata_test_data/ipsec.host3.json"
)

func init() {
	logrus.SetLevel(logrus.DebugLevel)
	runAllTestMetadataServers()
}

func runAllTestMetadataServers() {
	runTestMetadataServer2()
	runTestMetadataServer1()
	runTestMetadataServer3()
}

func runTestMetadataServer1() {
	runTestMetadataServer(answers1, metadataURL1, listenPort1, listenReloadPort1)
}

func runTestMetadataServer2() {
	runTestMetadataServer(answers2, metadataURL2, listenPort2, listenReloadPort2)
}

func runTestMetadataServer3() {
	runTestMetadataServer(answers3, metadataURL3, listenPort3, listenReloadPort3)
}

func runTestMetadataServer(answers, url, listenPort, listenReloadPort string) {
	logrus.Debugf("Starting Test Metadata Server")

	sc := rmd.NewServerConfig(
		answers,
		listenPort,
		listenReloadPort,
		true,
	)

	go func() { sc.Start() }()
}

//func TestGet

func TestMetadataStoreVsSimpleStore(t *testing.T) {
	logrus.Debugf("MetadataStore Vs SimpleStore")
	sDB1 := NewSimpleStore(simpleFile1, "")
	sDB1.Reload()

	clientIP1 := "10.42.231.44"
	mDB1, _ := NewMetadataStoreWithClientIP(metadataURL1, clientIP1)
	mDB1.Reload()

	metadataDB1Entries := mDB1.Entries()
	logrus.Debugf("len(metadataDB1Entries): %v", len(metadataDB1Entries))

	// Start comparing

	if !reflect.DeepEqual(sDB1.Entries(), mDB1.Entries()) {
		t.Error("expected Entries() to be equal")
	}

	if !reflect.DeepEqual(sDB1.PeerEntriesMap(), mDB1.PeerEntriesMap()) {
		t.Error("expected PeerEntriesMap() to be equal")
	}

	if !reflect.DeepEqual(sDB1.RemoteEntriesMap(), mDB1.RemoteEntriesMap()) {
		t.Error("expected RemoteEntriesMap() to be equal")
	}

	if !reflect.DeepEqual(sDB1.LocalHostIpAddress(), mDB1.LocalHostIpAddress()) {
		t.Error("expected LocalHostIpAddress() to be equal")
	}

	if !reflect.DeepEqual(sDB1.LocalIpAddress(), mDB1.LocalIpAddress()) {
		t.Error("expected LocalIpAddress() to be equal")
	}

	localIP := "10.42.114.70"
	remoteIP := "10.42.223.250"

	if sDB1.IsRemote(localIP) != mDB1.IsRemote(localIP) {
		t.Error("expected the lookup result of localIP to be same")
	}

	if sDB1.IsRemote(remoteIP) != mDB1.IsRemote(remoteIP) {
		t.Error("expected the lookup result of remoteIP to be same")
	}
}

func TestGetHostsMapFromHostsArray(t *testing.T) {
	mc, err := metadata.NewClientAndWait(metadataURL1)
	logrus.Debugf("mc: %v", mc)
	if err != nil {
		logrus.Errorf("couldn't create metadata client")
	}

	hosts, err := mc.GetHosts()
	if err != nil {
		t.Error("not expecting error, got :%v", err)
	}

	hostsMap := getHostsMapFromHostsArray(hosts)

	testUUID := "ce5d0147-8f2d-4e87-86ea-977dd61f83df"
	actual := hostsMap[testUUID].UUID

	if actual != testUUID {
		t.Error("expected ce5d0147-8f2d-4e87-86ea-977dd61f83df, got: %v", actual)
	}
}
