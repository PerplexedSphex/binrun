package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

// TerminalEngine interprets terminal.session.*.command messages.
// It publishes terse feedback events to event.terminal.session.*.freeze.
// Any side-effecting commands (script create/run) are forwarded to the existing COMMAND stream subjects.

type TerminalEngine struct {
	js jetstream.JetStream
}

func NewTerminalEngine(js jetstream.JetStream) *TerminalEngine {
	return &TerminalEngine{js: js}
}

var helpRe = regexp.MustCompile(`^\s*help\s*$`)

// Start creates a consumer on TERMINAL_CMD and blocks until ctx is done.
func (te *TerminalEngine) Start(ctx context.Context) error {
	// Ensure stream exists
	if _, err := te.js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "TERMINAL",
		Subjects: []string{"terminal.session.*.*"},
		Storage:  jetstream.FileStorage,
	}); err != nil {
		// if already exists ignore
		if !strings.Contains(err.Error(), "stream name already in use") {
			return fmt.Errorf("create TERMINAL stream: %w", err)
		}
	}

	// Consumer for command subjects
	cons, err := te.js.CreateOrUpdateConsumer(ctx, "TERMINAL", jetstream.ConsumerConfig{
		Durable:        "TERMINAL_CMD",
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: []string{"terminal.session.*.command"},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	_, err = cons.Consume(func(msg jetstream.Msg) {
		te.handleCommand(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}
	return nil
}

// commandPayload is what the browser posts to /terminal, forwarded unchanged.
type commandPayload struct {
	Cmd string `json:"cmd"`
}

type freezeEvent struct {
	Cmd    string `json:"cmd"`
	Output string `json:"output"`
}

func (te *TerminalEngine) handleCommand(ctx context.Context, msg jetstream.Msg) {
	var in commandPayload
	if err := json.Unmarshal(msg.Data(), &in); err != nil {
		slog.Warn("terminal: bad cmd payload", "err", err)
		_ = msg.Ack()
		return
	}
	sid := extractSessionFromTerminalSubject(msg.Subject())
	if sid == "" {
		_ = msg.Ack()
		return
	}

	outText := "ok"

	// Simple regex dispatch
	switch {
	case helpRe.MatchString(in.Cmd):
		outText = "commands: help, echo <text>, script create <name> <lang>, script run <name>"

	case strings.HasPrefix(in.Cmd, "echo "):
		outText = strings.TrimSpace(strings.TrimPrefix(in.Cmd, "echo "))

	case strings.HasPrefix(in.Cmd, "script create "):
		parts := strings.Split(in.Cmd, " ")
		if len(parts) < 4 {
			outText = "usage: script create <name> <lang>"
			break
		}
		name := parts[2]
		lang := parts[3]
		payload := map[string]any{
			"script_name":    name,
			"script_type":    lang,
			"correlation_id": in.Cmd,
		}
		_ = te.publishCommand(ctx, "command.script.create", payload)
		outText = "script create requested"

	case strings.HasPrefix(in.Cmd, "script run "):
		parts := strings.Split(in.Cmd, " ")
		if len(parts) < 3 {
			outText = "usage: script run <name> [args...]"
			break
		}
		name := parts[2]
		args := parts[3:]
		subj := fmt.Sprintf("command.script.%s.run", name)
		payload := map[string]any{
			"args":           args,
			"correlation_id": in.Cmd,
		}
		_ = te.publishCommand(ctx, subj, payload)
		outText = "script run requested"

	default:
		outText = "error: unknown command"
	}

	evt := freezeEvent{Cmd: in.Cmd, Output: outText}
	data, _ := json.Marshal(evt)
	evtSubj := fmt.Sprintf("event.terminal.session.%s.freeze", sid)
	if _, err := te.js.Publish(ctx, evtSubj, data); err != nil {
		slog.Warn("terminal: publish feedback", "err", err)
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}

func (te *TerminalEngine) publishCommand(ctx context.Context, subj string, body map[string]any) error {
	data, _ := json.Marshal(body)
	_, err := te.js.Publish(ctx, subj, data)
	return err
}

func extractSessionFromTerminalSubject(subject string) string {
	// terminal.session.<sid>.command
	parts := strings.Split(subject, ".")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}
