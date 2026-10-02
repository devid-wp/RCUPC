// Command rcupc-host runs the RCUPC host.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"rcupc/host/server"
	"rcupc/pkg/config"
	"rcupc/pkg/logging"
)

func main() {
	log := logging.New(os.Stderr, logging.LevelInfo)

	cfg := config.DefaultHost()
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [flags]\n", os.Args[0])
		flag.PrintDefaults()
	}
	config.BindHost(flag.CommandLine, &cfg)
	flag.Parse()

	if err := cfg.Validate(); err != nil {
		log.Error("invalid configuration", "err", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := server.New(cfg, log)
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Error("host stopped", "err", err)
		os.Exit(1)
	}
}
