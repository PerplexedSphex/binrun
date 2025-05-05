package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"slices"

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

// helpText returns contextual help for a command. Empty cmd returns the general help overview.
func (te *TerminalEngine) helpText(cmd string) string {
	switch cmd {
	case "", "general":
		return `commands:
  help, -h               Show this help
  ls presets             List available presets
  ls preset <id>         Show subjects built by a preset
  load <presetID> [flags]   Load a preset for this session; flags depend on the preset
  echo <text>            Echo text back
  script create <name> <lang>  Create a script
  script run <name> [args...]  Run a script`
	case "load":
		return `load <presetID> [--key value ...]
Subscribe this session to the preset's subjects.
Example: load scriptsubs --script foo --job 42`
	case "ls":
		return `ls presets            List preset IDs
ls preset <id>       Show the subjects a preset subscribes to.`
	default:
		return fmt.Sprintf("no help available for %s", cmd)
	}
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

	parts := strings.Fields(in.Cmd)

	// --- help handling ---------------------------------------------------
	if len(parts) > 0 {
		// general "help" or explicit topic
		if parts[0] == "help" {
			topic := ""
			if len(parts) > 1 {
				topic = parts[1]
			}
			te.sendFreeze(ctx, sid, in.Cmd, te.helpText(topic), msg)
			return
		}
		// trailing -h / --help form
		if last := parts[len(parts)-1]; last == "-h" || last == "--help" {
			topic := parts[0]
			te.sendFreeze(ctx, sid, in.Cmd, te.helpText(topic), msg)
			return
		}
	}

	// Check for preset management commands
	if txt, handled := te.handlePresetCommand(ctx, sid, in.Cmd, parts); handled {
		te.sendFreeze(ctx, sid, in.Cmd, txt, msg)
		return
	}

	// Simple regex dispatch (non-preset, non-help)
	outText := "ok"

	switch {
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

	te.sendFreeze(ctx, sid, in.Cmd, outText, msg)
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

// helper to parse flags in form --key value
func parseFlags(parts []string) (map[string]string, []string) {
	flags := map[string]string{}
	positionals := []string{}
	i := 0
	for i < len(parts) {
		p := parts[i]
		if strings.HasPrefix(p, "--") {
			key := strings.TrimPrefix(p, "--")
			if i+1 < len(parts) {
				flags[key] = parts[i+1]
				i += 2
				continue
			}
		}
		positionals = append(positionals, p)
		i++
	}
	return flags, positionals
}

func (te *TerminalEngine) handlePresetCommand(ctx context.Context, sid string, cmd string, parts []string) (string, bool) {
	if len(parts) == 0 {
		return "", false
	}

	switch parts[0] {
	case "ls":
		// ls presets OR ls preset <id>
		if len(parts) >= 2 && parts[1] == "presets" {
			keys := make([]string, 0, len(Presets))
			for k := range Presets {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			return "available presets: " + strings.Join(keys, ", "), true
		}
		if len(parts) >= 3 && parts[1] == "preset" {
			key := parts[2]
			p, ok := Presets[key]
			if !ok {
				return "unknown preset", true
			}
			subj := p.Build(map[string]string{})
			return fmt.Sprintf("%s: %v", key, subj), true
		}
	case "load":
		if len(parts) < 2 {
			return "usage: load <presetID> [--key value ...]", true
		}
		key := parts[1]
		p, ok := Presets[key]
		if !ok {
			return "unknown preset", true
		}

		flagArgs, _ := parseFlags(parts[2:])
		subs := p.Build(flagArgs)

		// include terminal freeze sub
		term := fmt.Sprintf("event.terminal.session.%s.freeze", sid)
		if !slices.Contains(subs, term) {
			subs = append(subs, term)
		}
		slices.Sort(subs)

		kv, err := te.js.KeyValue(ctx, "sessions")
		if err != nil {
			return "kv error", true
		}
		info := struct {
			Subscriptions []string `json:"subscriptions"`
		}{Subscriptions: subs}
		data, _ := json.Marshal(info)
		if _, err := kv.Put(ctx, sid, data); err != nil {
			slog.Warn("terminal: preset kv put", "err", err)
			return "failed to load preset", true
		}
		return fmt.Sprintf("preset %s loaded (%d subjects)", key, len(subs)), true
	}

	return "", false
}

// sendFreeze publishes the freeze event and acks/naks appropriately.
func (te *TerminalEngine) sendFreeze(ctx context.Context, sid, originalCmd, output string, msg jetstream.Msg) {
	evt := freezeEvent{Cmd: originalCmd, Output: output}
	data, _ := json.Marshal(evt)
	evtSubj := fmt.Sprintf("event.terminal.session.%s.freeze", sid)
	if _, err := te.js.Publish(ctx, evtSubj, data); err != nil {
		slog.Warn("terminal: publish feedback", "err", err)
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}
