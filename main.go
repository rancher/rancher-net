package main

import (
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/rancher/rancher-net/arp"
	"github.com/rancher/rancher-net/backend"
	"github.com/rancher/rancher-net/backend/ipsec"
	"github.com/rancher/rancher-net/backend/vxlan"
	"github.com/rancher/rancher-net/mdchandler"
	"github.com/rancher/rancher-net/server"
	"github.com/rancher/rancher-net/store"
)

var (
	// VERSION Of the binary
	VERSION = "0.0.0-dev"
)

const (
	backendFlag         = "backend"
	backendNameIpsec    = "ipsec"
	backendNameVxlan    = "vxlan"
	metadataFlag        = "use-metadata"
	metadataAddressFlag = "metadata-address"
)

func main() {
	app := cli.NewApp()
	app.Version = VERSION
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name: "log",
		},
		cli.StringFlag{
			Name: "pid-file",
		},
		cli.StringFlag{
			Name:  "file, f",
			Value: "config.json",
		},
		cli.StringFlag{
			Name:  "ipsec-config, c",
			Value: ".",
			Usage: "Configuration directory",
		},
		cli.BoolTFlag{
			Name:  "gcm",
			Usage: "GCM mode Supported",
		},
		cli.StringFlag{
			Name: "charon-log",
		},
		cli.BoolFlag{
			Name: "charon-launch",
		},
		cli.BoolFlag{
			Name: "test-charon",
		},
		cli.BoolFlag{
			Name: "debug",
		},
		cli.StringFlag{
			Name:  "listen",
			Value: ":8111",
		},
		cli.StringFlag{
			Name: "local-ip, i",
		},
		cli.StringFlag{
			Name:   backendFlag,
			Value:  backendNameIpsec,
			Usage:  "backend to use: ipsec/vxlan",
			EnvVar: "RANCHER_NET_BACKEND",
		},
		cli.StringFlag{
			Name:   metadataAddressFlag,
			Value:  store.DefaultMetadataAddress,
			Usage:  "metadata address to use",
			EnvVar: "RANCHER_METADATA_ADDRESS",
		},
		cli.StringFlag{
			Name:   "ipsec-ike-sa-rekey-interval",
			Value:  ipsec.DefaultIkeSaRekeyInterval,
			Usage:  "IKE_SA rekey interval time",
			EnvVar: "IPSEC_IKE_SA_REKEY_INTERVAL",
		},
		cli.StringFlag{
			Name:   "ipsec-child-sa-rekey-interval",
			Value:  ipsec.DefaultChildSaRekeyInterval,
			Usage:  "CHILD_SA rekey interval time",
			EnvVar: "IPSEC_CHILD_SA_REKEY_INTERVAL",
		},
		cli.BoolFlag{
			Name:   metadataFlag,
			Usage:  "Use metadata instead of config file",
			EnvVar: "RANCHER_NET_USE_METADATA",
		},
		cli.StringFlag{
			Name:   "ipsec-replay-window-size",
			Value:  ipsec.DefaultReplayWindowSize,
			Usage:  "IPSec Replay Window Size",
			EnvVar: "IPSEC_REPLAY_WINDOW_SIZE",
		},
		cli.IntFlag{
			Name:   "vxlan-vtep-mtu",
			Value:  vxlan.DefaultVTEPMTU,
			EnvVar: "VXLAN_VTEP_MTU",
		},
	}
	app.Action = func(ctx *cli.Context) {
		if err := appMain(ctx); err != nil {
			logrus.Fatal(err)
		}
	}

	app.Run(os.Args)
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

func appMain(ctx *cli.Context) error {
	if ctx.GlobalBool("test-charon") {
		if err := ipsec.Test(); err != nil {
			log.Fatalf("Failed to talk to charon: %v", err)
		}
		os.Exit(0)
	}

	logFile := ctx.GlobalString("log")
	if logFile != "" {
		if output, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666); err != nil {
			logrus.Fatalf("Failed to log to file %s: %v", logFile, err)
		} else {
			logrus.SetOutput(output)
		}
	}

	pidFile := ctx.GlobalString("pid-file")
	if pidFile != "" {
		logrus.Infof("Writing pid %d to %s", os.Getpid(), pidFile)
		if err := ioutil.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			logrus.Fatalf("Failed to write pid file %s: %v", pidFile, err)
		}
	}

	if ctx.GlobalBool("debug") {
		logrus.SetLevel(logrus.DebugLevel)
	}

	backendToUse := ctx.GlobalString(backendFlag)
	validBackend := backendToUse == backendNameIpsec || backendToUse == backendNameVxlan
	if !validBackend {
		logrus.Fatalf("Invalid backend specified")
	}
	logrus.Infof("Using backend: %v", backendToUse)

	useMetadata := ctx.GlobalBool(metadataFlag)
	logrus.Infof("Using metadata: %v", useMetadata)

	done := make(chan error)

	var overlay backend.Backend
	if backendToUse == backendNameVxlan {
		vxlanOverlay, err := vxlan.NewOverlay(ctx.GlobalInt("vxlan-vtep-mtu"))
		if err != nil {
			return err
		}
		overlay = vxlanOverlay
		overlay.Start(true, "")
		go func() {
			done <- arp.ListenAndServeForVXLAN(vxlanOverlay, "eth0")
		}()
	} else {
		var db store.Store
		var err error
		if useMetadata {
			logrus.Infof("Reading info from metadata")
			db, err = store.NewMetadataStore(ctx.GlobalString(metadataAddressFlag))
			if err != nil {
				logrus.Errorf("Error creating metadata store: %v", err)
				return err
			}

		} else {
			logrus.Infof("Reading info from config file")
			db = store.NewSimpleStore(waitForFile(ctx.GlobalString("file")), "")
		}

		db.Reload()
		ipsecOverlay := ipsec.NewOverlay(ctx.GlobalString("ipsec-config"), db)
		ipsecOverlay.ReplayWindowSize = ctx.GlobalString("ipsec-replay-window-size")
		ipsecOverlay.IPSecIkeSaRekeyInterval = ctx.GlobalString("ipsec-ike-sa-rekey-interval")
		ipsecOverlay.IPSecChildSaRekeyInterval = ctx.GlobalString("ipsec-child-sa-rekey-interval")
		if !ctx.GlobalBool("gcm") {
			ipsecOverlay.Blacklist = []string{"aes128gcm16"}
		}
		overlay = ipsecOverlay
		overlay.Start(ctx.GlobalBool("charon-launch"), ctx.GlobalString("charon-log"))

		go func() {
			done <- arp.ListenAndServe(db, "eth0")
		}()
	}

	listenPort := ctx.GlobalString("listen")
	logrus.Debugf("About to start server and listen on port: %v", listenPort)
	go func() {
		s := server.Server{
			Backend: overlay,
		}
		done <- s.ListenAndServe(listenPort)
	}()

	if err := overlay.Reload(); err != nil {
		logrus.Errorf("couldn't reload the overlay: %v", err)
		return err
	}

	if useMetadata && backendToUse == backendNameIpsec {
		go func() {
			mdch := mdchandler.NewMetadataChangeHandler(ctx.GlobalString(metadataAddressFlag), overlay)
			done <- mdch.Start()
		}()
	}

	return <-done
}
