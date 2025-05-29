package platform

import (
	"context"
	"log/slog"

	"binrun/internal/runtime"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func Run(ctx context.Context, nc *nats.Conn, ns *server.Server) {

	// 3. Initialize JetStream context (new API)
	js, err := jetstream.New(nc)
	if err != nil {
		slog.Error("JetStream context error", "err", err)
		return
	}

	// --- Activate ScriptRunner ---
	sr := runtime.NewScriptRunner(nc, js, "./scripts")
	go func() {
		if err := sr.Start(ctx); err != nil {
			slog.Error("ScriptRunner error", "err", err)
		} else {
			slog.Info("ScriptRunner started successfully")
		}
	}()
	// --- End ScriptRunner activation ---

	// --- Activate TerminalEngine ---
	te := runtime.NewTerminalEngine(js)
	go func() {
		if err := te.Start(ctx); err != nil {
			slog.Error("TerminalEngine error", "err", err)
		} else {
			slog.Info("TerminalEngine started successfully")
		}
	}()
	// --- End TerminalEngine activation ---

	// 4. Create JetStream streams for commands and events (new API).
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:      "COMMAND",
		Subjects:  []string{"command.>"},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		slog.Warn("Error adding COMMAND stream", "err", err)
	}
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "EVENT",
		Subjects: []string{"event.>"},
		Storage:  jetstream.FileStorage,
	})
	if err != nil {
		slog.Warn("Error adding EVENT stream", "err", err)
	}
	slog.Info("Streams 'COMMAND' and 'EVENT' created.")

	// 5. Create a Key-Value bucket "sessions" to track session subscriptions (new API).
	_, err = js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "sessions",
		History: 5,
		Storage: jetstream.FileStorage,
	})
	if err != nil {
		slog.Warn("Error creating KV bucket", "err", err)
	}
	slog.Info("KV bucket 'sessions' created for session subscription info.")

	// Create a Key-Value bucket "layouts" for saved layout configurations
	_, err = js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "layouts",
		History: 5,
		Storage: jetstream.FileStorage,
	})
	if err != nil {
		slog.Warn("Error creating layouts KV bucket", "err", err)
	}
	slog.Info("KV bucket 'layouts' created for saved layout configurations.")

	// The server is now configured and will automatically handle new sessions and messages.
	slog.Info("🚀 JetStream in-process system is up. You can now use NATS CLI to interact with it.")
	<-ctx.Done()
	slog.Info("Run: shutdown requested")
}
