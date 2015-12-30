package main

import (
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/rancher/rancher-net/arp"
	"github.com/rancher/rancher-net/backend/ipsec"
	"github.com/rancher/rancher-net/server"
	"github.com/rancher/rancher-net/store"
)

func main() {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "file, f",
			Value: "config.json",
		},
		cli.StringFlag{
			Name:  "config, c",
			Value: ".",
			Usage: "Configuration directory",
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
	}
	app.Action = func(ctx *cli.Context) {
		if err := appMain(ctx); err != nil {
			logrus.Fatal(err)
		}
	}

	app.Run(os.Args)
}

func appMain(ctx *cli.Context) error {
	if ctx.GlobalBool("debug") {
		logrus.SetLevel(logrus.DebugLevel)
	}

	db := store.NewSimpleStore(ctx.GlobalString("file"), ctx.GlobalString("local-ip"))
	overlay := ipsec.NewOverlay(ctx.GlobalString("config"), db)
	overlay.Start()
	if err := overlay.Reload(); err != nil {
		return err
	}

	done := make(chan error)
	go func() {
		done <- arp.ListenAndServe(db, "eth0")
	}()

	go func() {
		s := server.Server{
			Backend: overlay,
		}
		done <- s.ListenAndServe(ctx.GlobalString("listen"))
	}()

	return <-done
}
