package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/katerina7479/company_town/internal/daemon"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/session"
)

// runDaemon runs the daemon polling loop (blocking). Called by `ct daemon`.
func runDaemon() error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	if cfg.SessionPrefix != "" {
		session.SessionPrefix = cfg.SessionPrefix
	}

	d, err := daemon.New(conn, cfg)
	if err != nil {
		return err
	}

	// Handle signals for clean shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		fmt.Println("\nDaemon shutting down...")
		d.Stop()
	}()

	fmt.Printf("Daemon running (polling every %ds). Ctrl-C to stop.\n", cfg.PollingIntervalSeconds)
	d.Run()
	return nil
}
