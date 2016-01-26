package ipsec

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"os"
	"path"

	"github.com/Sirupsen/logrus"
	"github.com/bronze1man/goStrongswanVici"
)

const (
	ikeConfName     = "ike.conf"
	childSaConfName = "childsa.conf"
)

var (
	defaultIkeConf = []byte(`{
		"local_addrs": [],
		"proposals": ["aes128gcm16-sha256-modp2048", "aes-sha1-modp2048"],
		"encap": "yes",
		"local": {
			"auth": "psk"
		},
		"remote": {
			"auth": "psk"
		}
	}`)
	defaultChildSaConf = []byte(`{
		"local_ts": ["0.0.0.0/0"],
		"remote_ts": ["0.0.0.0/0"],
		"esp_proposals":  ["aes128gcm16-modp2048", "aes-modp2048"],
		"start_action": "start",
		"close_action": "start",
		"mode": "tunnel",
		"policies": "no"
	}`)
)

type Templates struct {
	ConfigDir           string
	ikeConfTemplate     []byte
	childSaConfTemplate []byte
	revision            string
}

func (t *Templates) Reload() error {
	var err error
	t.ikeConfTemplate, err = t.loadBytes(ikeConfName, defaultIkeConf)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(t.ikeConfTemplate, &goStrongswanVici.IKEConf{}); err != nil {
		logrus.Errorf("Failed to unmarshal: %v\n\t%s", err, string(t.ikeConfTemplate))
		return err
	}

	t.childSaConfTemplate, err = t.loadBytes(childSaConfName, defaultChildSaConf)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(t.childSaConfTemplate, &goStrongswanVici.ChildSAConf{}); err != nil {
		logrus.Errorf("Failed to unmarshal: %v\n\t%s", err, string(t.childSaConfTemplate))
		return err
	}

	digest := sha1.New()
	digest.Write(t.ikeConfTemplate)
	digest.Write(t.childSaConfTemplate)
	t.revision = hex.EncodeToString(digest.Sum(nil))

	return nil
}

func (t *Templates) Revision() string {
	return t.revision
}

func (t *Templates) NewIkeConf() goStrongswanVici.IKEConf {
	var resp goStrongswanVici.IKEConf
	// Should never fail because we checked this in Reload()
	json.Unmarshal(t.ikeConfTemplate, &resp)
	return resp
}

func (t *Templates) NewChildSaConf() goStrongswanVici.ChildSAConf {
	var resp goStrongswanVici.ChildSAConf
	// Should never fail because we checked this in Reload()
	json.Unmarshal(t.childSaConfTemplate, &resp)
	return resp
}

func (t *Templates) loadBytes(file string, defaultBytes []byte) ([]byte, error) {
	bytes, err := ioutil.ReadFile(path.Join(t.ConfigDir, file))
	if os.IsNotExist(err) {
		return defaultBytes, nil
	}
	return bytes, err
}
