package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/admin8800/s-ui/app"
	"github.com/admin8800/s-ui/cmd"
)

func runApp() {
	app := app.NewApp()

	err := app.Init()
	if err != nil {
		log.Fatal(err)
	}

	err = app.Start()
	if err != nil {
		log.Fatal(err)
	}

	sigCh := make(chan os.Signal, 1)
	// Trap shutdown signals
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGUSR1)
	for {
		sig := <-sigCh

		switch sig {
		case syscall.SIGHUP:
			app.RestartApp()
		case syscall.SIGUSR1:
			if err := app.ReloadWebCert(); err != nil {
				log.Println("热加载证书失败：", err)
			}
		default:
			app.Stop()
			return
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		runApp()
		return
	} else {
		cmd.ParseCmd()
	}
}
