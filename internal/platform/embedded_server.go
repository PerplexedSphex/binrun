package platform

import (
	"context"
	"errors"
	"net/url"
	"time"

	"log/slog"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// EmbeddedServerConfig holds options for running the embedded server.
type EmbeddedServerConfig struct {
	InProcess       bool
	EnableLogging   bool
	JetStream       bool
	JetStreamDomain string
	LeafNodeURL     string // empty disables leaf node
	LeafNodeCreds   string // optional, only used if LeafNodeURL is set
	StoreDir        string // optional, for JetStream file storage
}

// RunEmbeddedServer starts an embedded NATS server with the given config and returns a client connection, the server instance, and an error channel.
func RunEmbeddedServer(ctx context.Context, cfg EmbeddedServerConfig) (*nats.Conn, *server.Server, <-chan error, error) {
	var leafRemotes []*server.RemoteLeafOpts
	if cfg.LeafNodeURL != "" {
		leafURL, err := url.Parse(cfg.LeafNodeURL)
		if err != nil {
			return nil, nil, nil, err
		}
		leafRemotes = []*server.RemoteLeafOpts{{
			URLs:        []*url.URL{leafURL},
			Credentials: cfg.LeafNodeCreds,
		}}
	}

	opts := &server.Options{
		ServerName:      "embedded_server",
		DontListen:      cfg.InProcess,
		JetStream:       cfg.JetStream,
		JetStreamDomain: cfg.JetStreamDomain,
		StoreDir:        cfg.StoreDir,
	}
	if len(leafRemotes) > 0 {
		opts.LeafNode = server.LeafNodeOpts{Remotes: leafRemotes}
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, nil, nil, err
	}
	if cfg.EnableLogging {
		ns.SetLogger(NewNATSServerLogger(slog.Default()), false, false)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		return nil, nil, nil, errors.New("NATS Server timeout")
	}

	clientOpts := []nats.Option{}
	if cfg.InProcess {
		clientOpts = append(clientOpts, nats.InProcessServer(ns))
	}

	nc, err := nats.Connect(ns.ClientURL(), clientOpts...)
	if err != nil {
		return nil, nil, nil, err
	}

	errCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		// Context cancellation is handled by the caller (e.g., via a deferred ns.Shutdown()).
		// Avoid shutting down the server twice, which can cause panics in the NATS
		// server when internal channels are closed more than once.
		errCh <- ctx.Err()
	}()

	return nc, ns, errCh, nil
}
