package core

import (
	"errors"
	"net/url"
	"time"

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

// RunEmbeddedServer starts an embedded NATS server with the given config and returns a client connection and the server instance.
func RunEmbeddedServer(cfg EmbeddedServerConfig) (*nats.Conn, *server.Server, error) {
	var leafRemotes []*server.RemoteLeafOpts
	if cfg.LeafNodeURL != "" {
		leafURL, err := url.Parse(cfg.LeafNodeURL)
		if err != nil {
			return nil, nil, err
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
		return nil, nil, err
	}
	if cfg.EnableLogging {
		ns.ConfigureLogger()
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		return nil, nil, errors.New("NATS Server timeout")
	}

	clientOpts := []nats.Option{}
	if cfg.InProcess {
		clientOpts = append(clientOpts, nats.InProcessServer(ns))
	}

	nc, err := nats.Connect(ns.ClientURL(), clientOpts...)
	if err != nil {
		return nil, nil, err
	}

	return nc, ns, nil
}
