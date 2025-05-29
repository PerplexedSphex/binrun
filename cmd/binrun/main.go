package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"binrun/internal/platform"
)

func main() {
	platform.InitMetrics()
	platform.InitLogger()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	appCfg := platform.LoadAppConfig()

	// --- Run embedded NATS server ---
	nc, ns, natErrCh, err := platform.RunEmbeddedServer(ctx, *appCfg.NatsCfg)
	if err != nil {
		slog.Error("Failed to start embedded server", "err", err)
		os.Exit(1)
	}
	defer nc.Close()
	defer ns.Shutdown()

	var httpErrCh <-chan error
	if !appCfg.Flags.Headless {
		httpErrCh = platform.RunHTTPServer(ctx, nc, *appCfg.HTTPSrvCfg)
	} else {
		// Create a dummy channel that never sends
		ch := make(chan error)
		httpErrCh = ch
	}

	// --- Start the platform handlers ---
	go func() {
		select {
		case err := <-natErrCh:
			slog.Error("Embedded server error", "err", err)
			cancel()
		case err := <-httpErrCh:
			slog.Error("HTTP server error", "err", err)
			cancel()
		}
	}()

	platform.Run(ctx, nc, ns)
}
