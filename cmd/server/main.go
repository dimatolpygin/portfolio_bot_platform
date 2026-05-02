package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/bots-platform/internal/app"
	"github.com/example/bots-platform/internal/config"
)

func main() {
	var modeFlag string

	flag.StringVar(&modeFlag, "mode", "", "runtime mode: server, worker, or all")
	flag.Parse()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if modeFlag != "" {
		cfg.Mode = config.Mode(modeFlag)
	}

	if !cfg.Mode.Valid() {
		log.Fatalf("invalid mode %q", cfg.Mode)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "bots-platform failed: %v\n", err)
		os.Exit(1)
	}
}

