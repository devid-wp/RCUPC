// Command rcupc-host runs the RCUPC host.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"

	"rcupc/host/server"
)

func main() {
	addr := flag.String("addr", ":9000", "TCP address to listen on")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := server.New(*addr, log)
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Error("host stopped", "err", err)
		os.Exit(1)
	}
}
