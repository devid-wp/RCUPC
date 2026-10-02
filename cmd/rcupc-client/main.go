// Command rcupc-client runs the RCUPC client.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"rcupc/client/app"
	"rcupc/pkg/config"
	"rcupc/pkg/logging"
)

func main() {
	log := logging.New(os.Stderr, logging.LevelInfo)

	cfg := config.DefaultClient()
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [flags] <host>:<port>\n", os.Args[0])
		flag.PrintDefaults()
	}
	config.BindClient(flag.CommandLine, &cfg)
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	cfg.HostAddr = flag.Arg(0)

	if err := cfg.Validate(); err != nil {
		log.Error("invalid configuration", "err", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := app.Run(ctx, cfg, log); err != nil {
		log.Error("client stopped", "err", err)
		os.Exit(1)
	}
}
