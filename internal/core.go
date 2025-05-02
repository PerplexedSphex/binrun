package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// SessionInfo represents the value stored in the "sessions" KV bucket for each session.
type SessionInfo struct {
	Subscriptions []string `json:"subscriptions"`
}

// SubjectConsumer tracks an ephemeral consumer (push) per subject and its subscribers.
type SubjectConsumer struct {
	Subject  string
	Consumer jetstream.Consumer
	Subs     map[string]jetstream.ConsumeContext // sessionID -> ConsumeContext
}

// SimConfig controls the size and complexity of the simulation.
type SimConfig struct {
	NumSessions           int
	NumSubjectsPerSession int
	NumEventsPerSubject   int
	NumCommands           int
	SessionChurn          int
	InspectionLevel       int // 0=summary, 1=per-session, 2=per-message
}

func Run(ctx context.Context, nc *nats.Conn, ns *server.Server) {

	// 3. Initialize JetStream context (new API)
	js, err := jetstream.New(nc)
	if err != nil {
		slog.Error("JetStream context error", "err", err)
		return
	}

	// --- Activate ScriptRunner ---
	sr := NewScriptRunner(nc, js, "./scripts")
	go func() {
		if err := sr.Start(ctx); err != nil {
			slog.Error("ScriptRunner error", "err", err)
		} else {
			slog.Info("ScriptRunner started successfully")
		}
	}()
	// --- End ScriptRunner activation ---

	// --- Activate TerminalEngine ---
	te := NewTerminalEngine(js)
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
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "sessions",
		History: 5,
		Storage: jetstream.FileStorage,
	})
	if err != nil {
		slog.Warn("Error creating KV bucket", "err", err)
	}
	slog.Info("KV bucket 'sessions' created for session subscription info.")

	// 6. Set up a watcher on the entire "sessions" bucket to catch changes (new API).
	watcher, err := kv.Watch(ctx, jetstream.AllKeys)
	if err != nil {
		slog.Error("Failed to start KV watcher", "err", err)
		return
	}
	defer watcher.Stop()

	// 7. Goroutine to handle KV updates and adjust consumers/subscribers.
	go func() {
		for update := range watcher.Updates() {
			if update == nil {
				slog.Info("KV watcher initialized (current session states applied).")
				continue
			}
			sessionID := update.Key()
			if update.Operation() == jetstream.KeyValueDelete {
				_ = js.DeleteConsumer(ctx, "EVENT", sessionID)
				continue
			}
			var info SessionInfo
			if err := json.Unmarshal(update.Value(), &info); err != nil {
				slog.Warn("Invalid session info", "sessionID", sessionID, "err", err)
				continue
			}
			// Always delete old consumer (ignore error if not found)
			_ = js.DeleteConsumer(ctx, "EVENT", sessionID)
			if len(info.Subscriptions) == 0 {
				continue
			}
			cons, err := js.CreateOrUpdateConsumer(ctx, "EVENT", jetstream.ConsumerConfig{
				Durable:        sessionID,
				AckPolicy:      jetstream.AckNonePolicy,
				FilterSubjects: info.Subscriptions,
			})
			if err != nil {
				slog.Error("Could not create consumer for session", "sessionID", sessionID, "err", err)
				continue
			}
			_, err = cons.Consume(func(msg jetstream.Msg) {
				slog.Info("Session received message", "sessionID", sessionID, "subject", msg.Subject(), "data", string(msg.Data()))
			})
			if err != nil {
				slog.Error("Consume failed for session", "sessionID", sessionID, "err", err)
			}
		}
	}()

	// 8. Create a durable work-queue consumer on "command.x" (as an example command processor).

	cons, err := js.CreateOrUpdateConsumer(ctx, "COMMAND", jetstream.ConsumerConfig{
		Durable:        "COMMAND_X",
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: []string{"command.x"},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		slog.Error("Error creating work-queue consumer", "err", err)
		return
	}
	_, err = cons.Consume(func(msg jetstream.Msg) {
		slog.Info("Processing command.x message", "data", string(msg.Data()))
		time.Sleep(100 * time.Millisecond)
		msg.Ack()
	})
	if err != nil {
		slog.Error("Error subscribing to work-queue consumer", "err", err)
		return
	}
	slog.Info("Durable work-queue consumer 'COMMAND_X' (subject 'command.x') created and subscribed.")

	// 9. Create a mirrored stream to monitor post-processed command.x messages.
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "COMMANDX_MIRROR",
		Mirror: &jetstream.StreamSource{
			Name:          "COMMAND",
			FilterSubject: "command.x",
		},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
	})
	if err != nil {
		slog.Error("Error creating mirrored stream", "err", err)
		return
	}
	slog.Info("Stream 'COMMANDX_MIRROR' created (mirroring all 'command.x' messages from COMMAND).")

	// 10. (Optional) Additional stream transformations can be configured here if supported by your NATS server version.

	// The server is now configured and will automatically handle new sessions and messages.
	slog.Info("🚀 JetStream in-process system is up. You can now use NATS CLI to interact with it.")
	<-ctx.Done()
	slog.Info("Run: shutdown requested")
}

// Sim runs a deterministic, parameterized scenario against a fresh in-memory NATS+JetStream server.
func Sim(ctx context.Context, cfg SimConfig) error {
	slog.Info("Sim: starting scenario with", "cfg", cfg)

	// 1. Start a new in-memory, in-process NATS server
	ns, err := server.NewServer(&server.Options{
		JetStream:  true,
		StoreDir:   "", // memory store
		DontListen: true,
	})
	if err != nil {
		return fmt.Errorf("Sim: failed to start in-memory NATS server: %w", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		return errors.New("Sim: NATS server not ready")
	}
	defer ns.Shutdown()

	// 2. Connect a client
	nc, err := nats.Connect("", nats.InProcessServer(ns))
	if err != nil {
		return fmt.Errorf("Sim: failed to connect client: %w", err)
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("Sim: JetStream context error: %w", err)
	}

	// 3. Reset state: delete all streams/buckets you use
	_ = js.DeleteStream(ctx, "COMMAND")
	_ = js.DeleteStream(ctx, "EVENT")
	_ = js.DeleteStream(ctx, "COMMANDX_MIRROR")
	_ = js.DeleteKeyValue(ctx, "sessions")

	// Helper to create a stream, deleting if already exists
	createStream := func(cfg jetstream.StreamConfig) error {
		_, err := js.CreateStream(ctx, cfg)
		if err != nil && strings.Contains(err.Error(), "stream name already in use") {
			_ = js.DeleteStream(ctx, cfg.Name)
			_, err = js.CreateStream(ctx, cfg)
		}
		return err
	}

	// 4. Re-create streams and KV bucket as per cfg
	if err := createStream(jetstream.StreamConfig{
		Name:      "COMMAND",
		Subjects:  []string{"command.>"},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.MemoryStorage,
	}); err != nil {
		return fmt.Errorf("Sim: error creating COMMAND stream: %w", err)
	}
	if err := createStream(jetstream.StreamConfig{
		Name:     "EVENT",
		Subjects: []string{"event.>"},
		Storage:  jetstream.MemoryStorage,
	}); err != nil {
		return fmt.Errorf("Sim: error creating EVENT stream: %w", err)
	}
	if err := createStream(jetstream.StreamConfig{
		Name: "COMMANDX_MIRROR",
		Mirror: &jetstream.StreamSource{
			Name:          "COMMAND",
			FilterSubject: "command.x",
		},
		Storage:   jetstream.MemoryStorage,
		Retention: jetstream.LimitsPolicy,
	}); err != nil {
		return fmt.Errorf("Sim: error creating COMMANDX_MIRROR: %w", err)
	}
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "sessions",
		History: 5,
		Storage: jetstream.MemoryStorage,
	})
	if err != nil {
		return fmt.Errorf("Sim: error creating KV bucket: %w", err)
	}
	slog.Info("Sim: environment reset complete")

	subjects := make([]string, cfg.NumSubjectsPerSession)
	for j := 0; j < cfg.NumSubjectsPerSession; j++ {
		subjects[j] = fmt.Sprintf("event.topic.%d", j)
	}

	// 5. Create sessions with subscriptions
	for i := 0; i < cfg.NumSessions; i++ {
		sessionID := fmt.Sprintf("session-%d", i)
		info := SessionInfo{Subscriptions: subjects}
		_, err := kv.Put(ctx, sessionID, mustJSON(info))
		if err != nil {
			return fmt.Errorf("Sim: failed to put session %s: %w", sessionID, err)
		}
	}
	slog.Info("Sim: created", "numSessions", cfg.NumSessions)

	// 6. Publish events to each subject
	for j := 0; j < cfg.NumSubjectsPerSession; j++ {
		subj := subjects[j]
		for k := 0; k < cfg.NumEventsPerSubject; k++ {
			msg := []byte(fmt.Sprintf("msg-%d", k))
			if _, err := js.Publish(ctx, subj, msg); err != nil {
				return fmt.Errorf("Sim: publish event %s: %w", subj, err)
			}
		}
	}
	slog.Info("Sim: published", "numEventsPerSubject", cfg.NumEventsPerSubject, "numSubjectsPerSession", cfg.NumSubjectsPerSession)

	// 7. Publish command.x messages
	for l := 0; l < cfg.NumCommands; l++ {
		if _, err := js.Publish(ctx, "command.x", []byte(fmt.Sprintf("cmd-%d", l))); err != nil {
			return fmt.Errorf("Sim: publish command.x: %w", err)
		}
	}
	slog.Info("Sim: published", "numCommands", cfg.NumCommands)

	// 8. Churn: randomly delete sessions
	for i := 0; i < cfg.SessionChurn; i++ {
		sessionID := fmt.Sprintf("session-%d", rand.Intn(cfg.NumSessions))
		if err := kv.Delete(ctx, sessionID); err != nil {
			slog.Warn("Sim: warning: failed to delete session", "sessionID", sessionID, "err", err)
		}
	}
	slog.Info("Sim: churned", "numSessions", cfg.SessionChurn)

	// 9. Inspection/assertions
	// Wait for all messages to be processed (simple sleep, could poll for more accuracy)
	time.Sleep(500 * time.Millisecond)

	// Check COMMANDX_MIRROR has expected number of messages
	mirror, err := js.Stream(ctx, "COMMANDX_MIRROR")
	if err != nil {
		return fmt.Errorf("Sim: get COMMANDX_MIRROR: %w", err)
	}
	minfo, err := mirror.Info(ctx)
	if err != nil {
		return fmt.Errorf("Sim: mirror info: %w", err)
	}
	if minfo.State.Msgs != uint64(cfg.NumCommands) {
		return fmt.Errorf("Sim: mirror has %d msgs, want %d", minfo.State.Msgs, cfg.NumCommands)
	}
	slog.Info("Sim: mirror stream has correct message count", "msgCount", minfo.State.Msgs)

	if cfg.InspectionLevel > 0 {
		// For each session, check that it exists or was deleted, and optionally check event delivery
		for i := 0; i < cfg.NumSessions; i++ {
			sessionID := fmt.Sprintf("session-%d", i)
			_, err := kv.Get(ctx, sessionID)
			if err != nil && cfg.SessionChurn > 0 {
				slog.Info("Sim: session deleted (expected if churned)", "sessionID", sessionID)
				continue
			} else if err != nil {
				return fmt.Errorf("Sim: session %s missing: %w", sessionID, err)
			}
			if cfg.InspectionLevel > 1 {
				// Optionally, check event delivery for this session (not implemented: would require per-session durable consumer or pull)
			}
		}
		slog.Info("Sim: session existence checks complete")
	}

	slog.Info("Sim: scenario PASSED")
	return nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
