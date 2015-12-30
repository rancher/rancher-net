package ipsec

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/bronze1man/goStrongswanVici"
	"github.com/rancher/rancher-net/store"
	"github.com/vishvananda/netlink"
)

const (
	reqId    = 1234
	reqIdStr = "1234"
	pskFile  = "psk.txt"
)

type Overlay struct {
	sync.Mutex

	keyAttempt  map[string]bool
	hostAttempt map[string]bool
	keys        map[string]string
	hosts       map[string]string
	templates   Templates
	db          store.Store
	psk         string
}

func NewOverlay(configDir string, db store.Store) *Overlay {
	return &Overlay{
		db: db,
		templates: Templates{
			ConfigDir: configDir,
		},
		keys:  map[string]string{},
		hosts: map[string]string{},
	}
}

func (o *Overlay) Start() {
	go runCharon()
}

func (o *Overlay) Reload() error {
	if err := o.db.Reload(); err != nil {
		return err
	}

	content, err := ioutil.ReadFile(path.Join(o.templates.ConfigDir, pskFile))
	if err != nil {
		return err
	}
	o.psk = strings.TrimSpace(string(content))

	return o.configure()
}

func runCharon() {
	// Ignore error
	os.Remove("/var/run/charon.vici")

	args := []string{}
	for _, i := range strings.Split("dmn|mgr|ike|chd|cfg|knl|net|asn|tnc|imc|imv|pts|tls|esp|lib", "|") {
		args = append(args, "--debug-"+i)
		if logrus.GetLevel() == logrus.DebugLevel {
			args = append(args, "3")
		} else {
			args = append(args, "1")
		}
	}

	cmd := exec.Command("charon", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}

	logrus.Fatalf("charon exited: %v", cmd.Run())
}

func handleErr(firstErr, err error, fmt string, args ...interface{}) error {
	logrus.Errorf(fmt, args...)
	if firstErr != nil {
		return firstErr
	}
	return err
}

func (o *Overlay) configure() error {
	o.Lock()
	defer o.Unlock()

	if err := o.templates.Reload(); err != nil {
		return err
	}

	o.keyAttempt = map[string]bool{}
	o.hostAttempt = map[string]bool{}

	var firstErr error
	localHostIp := o.db.LocalHostIpAddress()
	hosts := map[string]bool{}

	policies, err := o.getRules()
	if err != nil {
		firstErr = handleErr(firstErr, err, "Failed to list rules for: %v", err)
	}

	for _, entry := range o.db.Entries() {
		if entry.Peer {
			if err := o.loadSharedKey(entry.IpAddress); err != nil {
				firstErr = handleErr(firstErr, err, "Failed to set PSK for peer agent %s: %v", entry.IpAddress, err)
			}
		}

		if localHostIp == entry.HostIpAddress {
			continue
		}
		if !hosts[entry.HostIpAddress] {
			if err := o.addHost(entry); err == nil {
				hosts[entry.HostIpAddress] = true
			} else {
				firstErr = handleErr(firstErr, err, "Failed to setup host %s: %v", entry.HostIpAddress, err)
			}
		}

		if err := o.addRules(entry, policies); err != nil {
			firstErr = handleErr(firstErr, err, "Failed to add rules for host %s, ip %s : %v", entry.HostIpAddress, entry.IpAddress, err)
		}
	}

	// Only purge when things worked
	if firstErr == nil {
		firstErr = o.removeHosts()
		// Currently VICI doesn't support unloading keys
	}

	if firstErr == nil {
		firstErr = o.deletePolicies(policies)
	}

	return firstErr
}

func (o *Overlay) deletePolicies(policies map[string]netlink.XfrmPolicy) error {
	var lastErr error
	for _, policy := range policies {
		if err := netlink.XfrmPolicyDel(&policy); err != nil {
			logrus.Errorf("Failed to delete policy: %+v, %v", policy, err)
			lastErr = err
		} else {
			logrus.Infof("Deleted policy: %+v", policy)
		}
	}
	return lastErr
}

func (o *Overlay) getRules() (map[string]netlink.XfrmPolicy, error) {
	policies := map[string]netlink.XfrmPolicy{}
	existing, err := netlink.XfrmPolicyList(0)
	if err != nil {
		return nil, err
	}

	for _, policy := range existing {
		if policy.Dir != netlink.XFRM_DIR_IN && policy.Dir != netlink.XFRM_DIR_FWD && policy.Dir != netlink.XFRM_DIR_OUT {
			continue
		}
		policies[toKey(&policy)] = policy
	}

	return policies, nil
}

func (o *Overlay) removeHosts() error {
	var firstErr error

	for k, _ := range o.hosts {
		if !o.hostAttempt[k] {
			if err := o.removeHost(k); err != nil {
				firstErr = handleErr(firstErr, err, "Failed to add remove connection for host %s: %v", k, err)
			} else {
				logrus.Infof("Removed connection for %s", k)
				delete(o.hosts, k)
			}
		}
	}

	return firstErr
}

func (o *Overlay) removeHost(host string) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	name := "conn-" + strings.Split(host, "/")[0]
	logrus.Infof("Removing connection for %s", name)
	return client.UnloadConn(&goStrongswanVici.UnloadConnRequest{
		Name: name,
	})
}

func getClient() (*goStrongswanVici.ClientConn, error) {
	var err error
	for i := 0; i < 3; i++ {
		var client *goStrongswanVici.ClientConn
		client, err = goStrongswanVici.NewClientConnFromDefaultSocket()
		if err == nil {
			return client, nil
		}

		if i > 0 {
			logrus.Errorf("Failed to connect to charon: %v", err)
		}
		time.Sleep(1 * time.Second)
	}

	return nil, err
}

func (o *Overlay) addHost(entry store.Entry) error {
	if err := o.loadSharedKey(entry.HostIpAddress); err != nil {
		return err
	}

	return o.addHostConnection(entry)
}

func (o *Overlay) loadSharedKey(ipAddress string) error {
	ipAddress = strings.Split(ipAddress, "/")[0]
	key := o.getPsk(ipAddress)

	o.keyAttempt[ipAddress] = true
	if o.keys[ipAddress] == key {
		logrus.Debugf("Key for %s already loaded", ipAddress)
		return nil
	}

	client, err := getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	sharedKey := &goStrongswanVici.Key{
		Typ:    "IKE",
		Data:   key,
		Owners: []string{ipAddress},
	}

	err = client.LoadShared(sharedKey)
	if err != nil {
		logrus.Infof("Failed to load pre-shared key for %s: %v", ipAddress, err)
		return err
	}

	o.keys[ipAddress] = key
	logrus.Infof("Loaded pre-shared key for %s", ipAddress)
	return nil
}

func (o *Overlay) addHostConnection(entry store.Entry) error {
	o.hostAttempt[entry.HostIpAddress] = true
	if o.hosts[entry.HostIpAddress] == o.templates.Revision() {
		logrus.Debugf("Connection already loaded for host %s", entry.HostIpAddress)
		return nil
	}

	client, err := getClient()
	if err != nil {
		return err
	}
	defer client.Close()

	childSAConf := o.templates.NewChildSaConf()
	childSAConf.ReqID = reqIdStr

	ikeConf := o.templates.NewIkeConf()
	ikeConf.RemoteAddrs = []string{entry.HostIpAddress}
	ikeConf.Children = map[string]goStrongswanVici.ChildSAConf{
		"child-" + entry.HostIpAddress: childSAConf,
	}

	name := fmt.Sprintf("conn-%s", entry.HostIpAddress)
	// Loading connections does seem to be very reliable, can't get info
	// why it's failing though.
	for i := 0; i < 3; i++ {
		err = client.LoadConn(&map[string]goStrongswanVici.IKEConf{
			name: ikeConf,
		})
		if err == nil {
			break
		}
	}
	if err != nil {
		logrus.Errorf("Failed loading connection %s: %v", name, err)
		return err
	}

	o.hosts[entry.HostIpAddress] = o.templates.Revision()
	logrus.Infof("Loaded connection: %v", name)
	return nil
}

func toKey(p *netlink.XfrmPolicy) string {
	buffer := bytes.Buffer{}
	buffer.WriteString(p.Dir.String())
	buffer.WriteRune('-')
	if p.Src != nil {
		buffer.WriteString(p.Src.String())
	}
	buffer.WriteRune('-')
	if p.Dst != nil {
		buffer.WriteString(p.Dst.String())
	}
	buffer.WriteRune('-')
	if len(p.Tmpls) > 0 {
		buffer.WriteString(p.Tmpls[0].Src.String())
		buffer.WriteRune('-')
		buffer.WriteString(p.Tmpls[0].Dst.String())
	}

	return buffer.String()
}

func (o *Overlay) addRules(entry store.Entry, policies map[string]netlink.XfrmPolicy) error {
	localIp := net.ParseIP(o.db.LocalIpAddress())
	remoteHostIp := net.ParseIP(entry.HostIpAddress)

	ip, ipNet, err := net.ParseCIDR(entry.IpAddress)
	if err != nil {
		return err
	}

	_, ipDirectNet, err := net.ParseCIDR(fmt.Sprintf("%s/32", ip))
	if err != nil {
		return err
	}

	outPolicy := netlink.XfrmPolicy{
		Src: ipNet,
		Dst: ipDirectNet,
		Dir: netlink.XFRM_DIR_OUT,
		Tmpls: []netlink.XfrmPolicyTmpl{
			{
				Src:   localIp,
				Dst:   remoteHostIp,
				Proto: netlink.XFRM_PROTO_ESP,
				Mode:  netlink.XFRM_MODE_TUNNEL,
				Reqid: reqId,
			},
		},
	}
	inPolicy := netlink.XfrmPolicy{
		Src: ipDirectNet,
		Dst: ipNet,
		Dir: netlink.XFRM_DIR_IN,
		Tmpls: []netlink.XfrmPolicyTmpl{
			{
				Src:   remoteHostIp,
				Dst:   localIp,
				Proto: netlink.XFRM_PROTO_ESP,
				Mode:  netlink.XFRM_MODE_TUNNEL,
				Reqid: reqId,
			},
		},
	}
	fwdPolicy := netlink.XfrmPolicy{
		Src: ipDirectNet,
		Dst: ipNet,
		Dir: netlink.XFRM_DIR_FWD,
		Tmpls: []netlink.XfrmPolicyTmpl{
			{
				Src:   remoteHostIp,
				Dst:   localIp,
				Proto: netlink.XFRM_PROTO_ESP,
				Mode:  netlink.XFRM_MODE_TUNNEL,
				Reqid: reqId,
			},
		},
	}

	var lastErr error
	for _, policy := range []netlink.XfrmPolicy{outPolicy, inPolicy, fwdPolicy} {
		key := toKey(&policy)
		if _, ok := policies[key]; !ok {
			if err := netlink.XfrmPolicyAdd(&policy); err != nil {
				logrus.Errorf("Failed to add policy: %+v, %v", policy, err)
				lastErr = err
			} else {
				logrus.Infof("Added policy: %+v", policy)
			}
		}
		delete(policies, key)
	}

	return lastErr
}

func (o *Overlay) getPsk(hostIp string) string {
	return o.psk
}
