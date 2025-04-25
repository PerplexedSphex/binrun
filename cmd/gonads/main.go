package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	core "binrun/internal"
)

func main() {
	appCfg := core.LoadAppConfig()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if appCfg.Flags.Sim {
		if err := core.Sim(ctx, *appCfg.SimCfg); err != nil {
			log.Fatalf("Sim failed: %v", err)
		}
		return
	}

	nc, ns, err := core.RunEmbeddedServer(*appCfg.NatsCfg)
	if err != nil {
		log.Fatalf("Failed to start embedded server: %v", err)
	}
	defer nc.Drain()

	core.Run(ctx, nc, ns)
	ns.Shutdown()
}
