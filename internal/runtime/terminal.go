package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"slices"

	"binrun/internal/messages"

	"github.com/nats-io/nats.go/jetstream"
)

// TerminalEngine interprets terminal.session.*.command messages.
// It publishes terse feedback events to event.terminal.session.*.freeze.
// Any side-effecting commands (script create/run) are forwarded to the existing COMMAND stream subjects.

type TerminalEngine struct {
	js        jetstream.JetStream
	publisher *messages.Publisher
}

func NewTerminalEngine(js jetstream.JetStream) *TerminalEngine {
	return &TerminalEngine{
		js:        js,
		publisher: messages.NewPublisher(js),
	}
}

// Start creates a consumer on TERMINAL_CMD and blocks until ctx is done.
func (te *TerminalEngine) Start(ctx context.Context) error {
	// Ensure stream exists
	if _, err := te.js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "TERMINAL",
		Subjects: []string{"terminal.command"},
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
		FilterSubjects: []string{messages.TerminalCommandSubject},
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

// helpText returns contextual help for a command. Empty cmd returns the general help overview.
func (te *TerminalEngine) helpText(cmd string) string {
	switch cmd {
	case "", "general":
		return `commands:
  help, -h               Show this help
  ls scripts             List available scripts
  ls presets             List available presets
  ls preset <id>         Show subjects built by a preset
  load <presetID> [flags]   Load a preset for this session; flags depend on the preset
  view readme            Show README in the left panel
  view <script>          Show script source in the left panel
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
	var in messages.TerminalCommandMessage
	if err := json.Unmarshal(msg.Data(), &in); err != nil {
		slog.Warn("terminal: bad cmd payload", "err", err)
		_ = msg.Ack()
		return
	}

	// Session ID now comes from the message body
	sid := in.SessionID
	if sid == "" {
		slog.Warn("terminal: missing session ID in message body")
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
	var docPath string       // Declare docPath here for broader scope
	var pathsToView []string // Declare here for access later

	switch {
	case strings.HasPrefix(in.Cmd, "view "):
		parts := strings.Split(in.Cmd, " ")
		if len(parts) < 2 {
			outText = "usage: view <doc|scriptname>"
			break
		}
		doc := strings.ToLower(parts[1])
		switch doc {
		case "readme", "readme.md":
			docPath = "README.md"
		default: // Assume it's a script name
			scriptName := parts[1] // Use original case
			scriptDir := filepath.Join("scripts", scriptName)
			info, err := os.Stat(scriptDir)
			if err != nil || !info.IsDir() {
				outText = fmt.Sprintf("script '%s' not found or not a directory", scriptName)
				break
			}

			// Find main script file
			mainFile := findMainScriptFile(scriptDir)
			if mainFile == "" {
				outText = fmt.Sprintf("main script file (main.py/go, index.js) not found in '%s'", scriptName)
				break
			}

			// Collect paths to view: README first, then main file.
			readmePath := filepath.Join(scriptDir, "README.md")
			if _, err := os.Stat(readmePath); err == nil {
				pathsToView = append(pathsToView, readmePath)
			}
			pathsToView = append(pathsToView, filepath.Join(scriptDir, mainFile))
			docPath = "script-view" // Use a placeholder to signal success
		}

		// Check if we found either a direct doc (README.md) or script files
		if docPath == "README.md" || docPath == "script-view" {
			// Publish viewdoc event so UI can render the markdown
			var pathsPayload []string
			if docPath == "README.md" {
				pathsPayload = []string{"README.md"}
			} else { // script-view
				pathsPayload = pathsToView
			}

			evt := messages.NewTerminalViewDocEvent(sid, pathsPayload)
			if err := te.publisher.PublishEvent(ctx, evt); err != nil {
				slog.Warn("terminal: publish viewdoc", "err", err)
			}

			// Ensure the session is subscribed to this subject
			kv, err := te.js.KeyValue(ctx, "sessions")
			if err == nil {
				entry, _ := kv.Get(ctx, sid)
				var info struct {
					Subscriptions []string `json:"subscriptions"`
				}
				if entry != nil {
					_ = json.Unmarshal(entry.Value(), &info)
				}
				evtSubj := messages.TerminalViewDocSubject(sid)
				added := false
				if !slices.Contains(info.Subscriptions, evtSubj) {
					info.Subscriptions = append(info.Subscriptions, evtSubj)
					slices.Sort(info.Subscriptions)
					added = true
				}
				if added {
					if data, err := json.Marshal(info); err == nil {
						if _, err := kv.Put(ctx, sid, data); err != nil {
							slog.Warn("terminal: kv put viewdoc", "err", err)
						}
					}
				}
			}

			if docPath == "README.md" {
				outText = "opening README.md"
			} else {
				outText = fmt.Sprintf("opening script %s (%d files)", parts[1], len(pathsPayload))
			}
		}

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
		cmd := messages.NewScriptCreateCommand(name, lang).WithCorrelation(in.Cmd)
		if err := te.publisher.PublishCommand(ctx, cmd); err != nil {
			slog.Error("Failed to publish script create command", "err", err)
			outText = "error: failed to create script"
		} else {
			outText = "script create requested"
		}

	case strings.HasPrefix(in.Cmd, "script run "):
		parts := strings.Split(in.Cmd, " ")
		if len(parts) < 3 {
			outText = "usage: script run <name> [args...]"
			break
		}
		name := parts[2]
		args := parts[3:]
		cmd := messages.NewScriptRunCommand(name).
			WithArgs(args...).
			WithCorrelation(in.Cmd)
		if err := te.publisher.PublishCommand(ctx, cmd); err != nil {
			slog.Error("Failed to publish script run command", "err", err)
			outText = "error: failed to run script"
		} else {
			outText = "script run requested"
		}

	default:
		outText = "error: unknown command"
	}

	// If docPath is still "", it means the switch default block failed to find a script
	if docPath == "" && outText == "ok" { // Check if docPath was set at all
		outText = "error: view target not found"
	}

	te.sendFreeze(ctx, sid, in.Cmd, outText, msg)
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
		if len(parts) >= 2 && parts[1] == "scripts" {
			dirs, err := os.ReadDir("./scripts")
			if err != nil {
				return "error reading scripts directory", true
			}
			var scriptList []string
			for _, entry := range dirs {
				if entry.IsDir() {
					scriptName := entry.Name()
					lang := guessScriptLang("./scripts/" + scriptName)
					if lang != "" {
						scriptList = append(scriptList, fmt.Sprintf("%s (%s)", scriptName, lang))
					} else {
						scriptList = append(scriptList, scriptName)
					}
				}
			}
			slices.Sort(scriptList)
			return "available scripts:\n  " + strings.Join(scriptList, "\n  "), true
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
		term := messages.TerminalFreezeSubject(sid)
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
	evt := messages.NewTerminalFreezeEvent(sid, originalCmd, output)
	if err := te.publisher.PublishEvent(ctx, evt); err != nil {
		slog.Warn("terminal: publish feedback", "err", err)
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}

// --- Helpers for ls scripts / view <script> ---

func guessScriptLang(scriptDir string) string {
	if _, err := os.Stat(filepath.Join(scriptDir, "main.py")); err == nil {
		return "python"
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "main.go")); err == nil {
		return "go"
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "index.ts")); err == nil {
		return "typescript"
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "index.js")); err == nil {
		return "javascript"
	}
	// Add more languages as needed
	return ""
}

func findMainScriptFile(scriptDir string) string {
	commonFiles := []string{"main.py", "main.go", "index.ts"}
	for _, f := range commonFiles {
		if _, err := os.Stat(filepath.Join(scriptDir, f)); err == nil {
			return f
		}
	}
	return ""
}
