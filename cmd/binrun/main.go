package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	core "binrun/internal"
)

func main() {
	core.InitLogger()
	core.InitMetrics()

	appCfg := core.LoadAppConfig()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if appCfg.Flags.Sim {
		if err := core.Sim(ctx, *appCfg.SimCfg); err != nil {
			slog.Error("Sim failed", "err", err)
		}
		return
	}

	nc, ns, natErrCh, err := core.RunEmbeddedServer(ctx, *appCfg.NatsCfg)
	if err != nil {
		slog.Error("embedded server start", "err", err)
		return
	}
	// error channel for NATS server shutdown is now natErrCh from RunEmbeddedServer

	var httpErrCh <-chan error
	if !appCfg.Flags.Headless {
		httpErrCh = core.RunHTTPServer(ctx, nc, *appCfg.HTTPSrvCfg)
	} else {
		slog.Info("Headless flag active – HTTP server not started")
		ch := make(chan error, 1)
		httpErrCh = ch
	}

	var runErrCh <-chan error
	{
		rc := make(chan error, 1)
		runErrCh = rc
		go func() {
			// start JetStream scaffolding; blocks until ctx done
			core.Run(ctx, nc, ns)
			rc <- ctx.Err()
		}()
	}

	select {
	case err := <-httpErrCh:
		if err != nil && err != context.Canceled {
			slog.Error("http server", "err", err)
		}
	case err := <-natErrCh:
		if err != nil && err != context.Canceled {
			slog.Error("nats", "err", err)
		}
	case err := <-runErrCh:
		if err != nil && err != context.Canceled {
			slog.Error("core run", "err", err)
		}
	}

	nc.Drain()
}
