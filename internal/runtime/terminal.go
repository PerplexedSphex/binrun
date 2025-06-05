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
		return `Quick Start Examples:
  script create mybot python     Create a Python script
  script run mybot --input '{"message":"hello"}'
  env set API_KEY=sk-123        Set environment variable
  view mybot                    View script source code
  
Common Commands:
  help [topic]           Show help (try: help script, help env)
  ls scripts             List available scripts
  script info <name>     Show script details and schemas
  script create <name> <lang>  Create script (python|typescript)
  script run <name> [options]  Run script with JSON input
  env set KEY=VALUE      Set session environment variable
  view <script> [what]   View script files/schemas/types
  
Navigation:
  ls presets             List available presets
  load <preset> [args]   Load a preset subscription
  echo <text>            Echo text back
  
For detailed help on any command, use: <command> --help`
	case "script":
		return `script commands:
  script create <name> <lang>   Create a new script (python|typescript)
  script run <name> [options]   Run a script with JSON input
  script info <name>            Show script details and schemas

Run Options:
  --input <json>            Inline JSON input
  --file <path>             Read JSON input from file
  --env KEY=VALUE           Set environment variable

Examples:
  script create mybot python
  script run mybot --input '{"message":"hello"}'
  script run mybot --file input.json --env API_KEY=xyz
  script info mybot`
	case "env":
		return `env commands:
  env set KEY=VALUE    Set an environment variable for this session
  env list             List all session environment variables
  env clear            Clear all session environment variables
  
Examples:
  env set API_KEY=sk-123456
  env set DATABASE_URL=postgres://localhost/mydb
  env list
  
Environment variables are layered in this order (highest priority first):
  1. Command-line --env flags
  2. Session env (set with 'env set')
  3. Script .env file
  4. Repository .env file
  5. OS environment`
	case "load":
		return `load <presetID> [--key value ...]
Subscribe this session to the preset's subjects.

Example: load scriptsubs --script foo --job 42`
	case "ls":
		return `ls commands:
  ls scripts           List available scripts with their types
  ls presets           List preset IDs
  ls preset <id>       Show the subjects a preset subscribes to

Examples:
  ls scripts
  ls presets
  ls preset scriptsubs`
	case "view":
		return `view commands:
  view readme                    Show README.md
  view <script>                  Show script source files
  view <script> schema [in|out]  Show script's JSON schemas
  view <script> types            Show generated type definitions
  view <script> env              Show script's .env file

Examples:
  view mybot                     # Shows README + main script file
  view mybot schema in           # Shows input schema
  view mybot types               # Shows generated TypeScript/Python types`
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

	// Check for env commands
	if txt, handled := te.handleEnvCommand(ctx, sid, in.Cmd, parts); handled {
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
			outText = "usage: view <doc|scriptname> [schema|types|env]"
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

			// Check for subcommands
			if len(parts) > 2 {
				switch parts[2] {
				case "schema":
					target := "in"
					if len(parts) > 3 {
						target = parts[3]
					}
					schemaFile := fmt.Sprintf("%s.schema.json", target)
					schemaPath := filepath.Join(scriptDir, schemaFile)
					if _, err := os.Stat(schemaPath); err != nil {
						outText = fmt.Sprintf("schema file not found: %s", schemaFile)
						break
					}
					pathsToView = []string{schemaPath}
					docPath = "schema-view"
					outText = fmt.Sprintf("opening %s schema for %s", target, scriptName)
				case "types":
					typesDir := filepath.Join(scriptDir, "types")
					if _, err := os.Stat(typesDir); err != nil {
						outText = "types directory not found (run script create first)"
						break
					}
					// Find all type files
					entries, _ := os.ReadDir(typesDir)
					for _, e := range entries {
						if !e.IsDir() {
							pathsToView = append(pathsToView, filepath.Join(typesDir, e.Name()))
						}
					}
					if len(pathsToView) == 0 {
						outText = "no type files found"
						break
					}
					docPath = "types-view"
					outText = fmt.Sprintf("opening %d type files for %s", len(pathsToView), scriptName)
				case "env":
					envPath := filepath.Join(scriptDir, ".env")
					if _, err := os.Stat(envPath); err != nil {
						outText = "no .env file found for this script"
						break
					}
					pathsToView = []string{envPath}
					docPath = "env-view"
					outText = fmt.Sprintf("opening .env for %s", scriptName)
				default:
					outText = "unknown subcommand: " + parts[2]
				}
			} else {
				// Default view behavior
				mainFile := findMainScriptFile(scriptDir)
				if mainFile == "" {
					outText = fmt.Sprintf("main script file not found in '%s'", scriptName)
					break
				}
				// Collect paths: README first, then main file
				readmePath := filepath.Join(scriptDir, "README.md")
				if _, err := os.Stat(readmePath); err == nil {
					pathsToView = append(pathsToView, readmePath)
				}
				pathsToView = append(pathsToView, filepath.Join(scriptDir, mainFile))
				docPath = "script-view"
			}
		}

		// Publish viewdoc event if we have paths
		if len(pathsToView) > 0 || docPath == "README.md" {
			var pathsPayload []string
			if docPath == "README.md" {
				pathsPayload = []string{"README.md"}
			} else {
				pathsPayload = pathsToView
			}

			evt := messages.NewTerminalViewDocEvent(sid, pathsPayload)
			if err := te.publisher.PublishEvent(ctx, evt); err != nil {
				slog.Warn("terminal: publish viewdoc", "err", err)
			}

			// Ensure session is subscribed
			te.ensureSessionSubscribed(ctx, sid, messages.TerminalViewDocSubject(sid))

			if docPath == "README.md" && outText == "ok" {
				outText = "opening README.md"
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
		outText = te.handleScriptRun(ctx, sid, in.Cmd)

	case strings.HasPrefix(in.Cmd, "script info "):
		parts := strings.Split(in.Cmd, " ")
		if len(parts) < 3 {
			outText = "usage: script info <name>"
			break
		}
		scriptName := parts[2]
		outText = te.getScriptInfo(scriptName)

	default:
		outText = "error: unknown command"
	}

	// If docPath is still "", it means the switch default block failed to find a script
	if docPath == "" && outText == "ok" { // Check if docPath was set at all
		outText = "error: view target not found"
	}

	te.sendFreeze(ctx, sid, in.Cmd, outText, msg)
}

// handleScriptRun processes the script run command with new JSON input and env support
func (te *TerminalEngine) handleScriptRun(ctx context.Context, sid, cmdStr string) string {
	// Parse command line
	tokens := tokenizeCommand(cmdStr)
	if len(tokens) < 3 {
		return "usage: script run <name> [--input <json>] [--file <path>] [--env KEY=VALUE ...]"
	}

	scriptName := tokens[2]
	var input json.RawMessage
	envOverrides := make(map[string]string)

	// Get session env vars
	sessionEnv := te.getSessionEnv(ctx, sid)
	for k, v := range sessionEnv {
		envOverrides[k] = v
	}

	// Parse options
	i := 3
	for i < len(tokens) {
		switch tokens[i] {
		case "--input":
			if i+1 >= len(tokens) {
				return "error: --input requires a value"
			}
			input = json.RawMessage(tokens[i+1])
			// Validate JSON
			var test interface{}
			if err := json.Unmarshal(input, &test); err != nil {
				return fmt.Sprintf("error: invalid JSON input: %v", err)
			}
			i += 2
		case "--file":
			if i+1 >= len(tokens) {
				return "error: --file requires a path"
			}
			data, err := os.ReadFile(tokens[i+1])
			if err != nil {
				return fmt.Sprintf("error: cannot read file: %v", err)
			}
			input = json.RawMessage(data)
			// Validate JSON
			var test interface{}
			if err := json.Unmarshal(input, &test); err != nil {
				return fmt.Sprintf("error: invalid JSON in file: %v", err)
			}
			i += 2
		case "--env":
			if i+1 >= len(tokens) {
				return "error: --env requires KEY=VALUE"
			}
			parts := strings.SplitN(tokens[i+1], "=", 2)
			if len(parts) != 2 {
				return "error: --env format is KEY=VALUE"
			}
			envOverrides[parts[0]] = parts[1]
			i += 2
		default:
			return fmt.Sprintf("error: unknown option: %s", tokens[i])
		}
	}

	// Default to empty object if no input provided
	if input == nil {
		input = json.RawMessage("{}")
	}

	// Create and publish the command
	cmd := messages.NewScriptRunCommand(scriptName).
		WithInput(input).
		WithEnv(envOverrides).
		WithCorrelation(cmdStr)

	if err := te.publisher.PublishCommand(ctx, cmd); err != nil {
		slog.Error("Failed to publish script run command", "err", err)
		return "error: failed to run script"
	}

	return fmt.Sprintf("running %s with %d bytes of input and %d env overrides", scriptName, len(input), len(envOverrides))
}

// getScriptInfo returns formatted information about a script
func (te *TerminalEngine) getScriptInfo(scriptName string) string {
	scriptDir := filepath.Join("scripts", scriptName)
	info, err := os.Stat(scriptDir)
	if err != nil || !info.IsDir() {
		return fmt.Sprintf("script '%s' not found", scriptName)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Script: %s", scriptName))

	// Detect language
	lang := guessScriptLang(scriptDir)
	if lang != "" {
		parts = append(parts, fmt.Sprintf("Language: %s", lang))
	}

	// Check for schemas
	inSchema := filepath.Join(scriptDir, "in.schema.json")
	outSchema := filepath.Join(scriptDir, "out.schema.json")
	hasIn := fileExists(inSchema)
	hasOut := fileExists(outSchema)

	if hasIn || hasOut {
		parts = append(parts, "Schemas:")
		if hasIn {
			// Try to read and show schema title/description
			if data, err := os.ReadFile(inSchema); err == nil {
				var schema map[string]interface{}
				if json.Unmarshal(data, &schema) == nil {
					desc := "  Input: "
					if title, ok := schema["title"].(string); ok {
						desc += title
					} else {
						desc += "defined"
					}
					if description, ok := schema["description"].(string); ok {
						desc += fmt.Sprintf(" - %s", description)
					}
					parts = append(parts, desc)
				}
			}
		}
		if hasOut {
			// Try to read and show schema title/description
			if data, err := os.ReadFile(outSchema); err == nil {
				var schema map[string]interface{}
				if json.Unmarshal(data, &schema) == nil {
					desc := "  Output: "
					if title, ok := schema["title"].(string); ok {
						desc += title
					} else {
						desc += "defined"
					}
					if description, ok := schema["description"].(string); ok {
						desc += fmt.Sprintf(" - %s", description)
					}
					parts = append(parts, desc)
				}
			}
		}
	}

	// Check for .env file
	if fileExists(filepath.Join(scriptDir, ".env")) {
		parts = append(parts, "Environment: .env file present")
	}

	// Check for types
	typesDir := filepath.Join(scriptDir, "types")
	if info, err := os.Stat(typesDir); err == nil && info.IsDir() {
		entries, _ := os.ReadDir(typesDir)
		typeCount := 0
		for _, e := range entries {
			if !e.IsDir() {
				typeCount++
			}
		}
		if typeCount > 0 {
			parts = append(parts, fmt.Sprintf("Types: %d generated files", typeCount))
		}
	}

	parts = append(parts, "", "Commands:")
	parts = append(parts, fmt.Sprintf("  view %s              - View source", scriptName))
	parts = append(parts, fmt.Sprintf("  view %s schema       - View schemas", scriptName))
	parts = append(parts, fmt.Sprintf("  view %s types        - View generated types", scriptName))
	parts = append(parts, fmt.Sprintf("  script run %s --help - Run help", scriptName))

	return strings.Join(parts, "\n")
}

// handleEnvCommand handles environment variable commands
func (te *TerminalEngine) handleEnvCommand(ctx context.Context, sid, cmd string, parts []string) (string, bool) {
	if len(parts) == 0 || parts[0] != "env" {
		return "", false
	}

	if len(parts) == 1 {
		return "usage: env [set|list|clear]", true
	}

	kv, err := te.js.KeyValue(ctx, "sessions")
	if err != nil {
		return "error: cannot access session store", true
	}

	switch parts[1] {
	case "set":
		if len(parts) < 3 {
			return "usage: env set KEY=VALUE", true
		}
		// Join all parts after "env set" to handle values with spaces
		envStr := strings.Join(parts[2:], " ")
		eqIdx := strings.Index(envStr, "=")
		if eqIdx == -1 {
			return "error: format is KEY=VALUE", true
		}
		key := envStr[:eqIdx]
		value := envStr[eqIdx+1:]

		// Get current session data
		var info SessionInfo
		if entry, err := kv.Get(ctx, sid); err == nil && entry != nil {
			_ = json.Unmarshal(entry.Value(), &info)
		}
		if info.Env == nil {
			info.Env = make(map[string]string)
		}
		info.Env[key] = value

		// Save back
		data, _ := json.Marshal(info)
		if _, err := kv.Put(ctx, sid, data); err != nil {
			return "error: failed to save env var", true
		}
		return fmt.Sprintf("set %s=%s", key, value), true

	case "list":
		env := te.getSessionEnv(ctx, sid)
		if len(env) == 0 {
			return "no session environment variables set", true
		}
		var lines []string
		for k, v := range env {
			lines = append(lines, fmt.Sprintf("%s=%s", k, v))
		}
		slices.Sort(lines)
		return "session environment:\n  " + strings.Join(lines, "\n  "), true

	case "clear":
		// Get current session data
		var info SessionInfo
		if entry, err := kv.Get(ctx, sid); err == nil && entry != nil {
			_ = json.Unmarshal(entry.Value(), &info)
		}
		info.Env = nil

		// Save back
		data, _ := json.Marshal(info)
		if _, err := kv.Put(ctx, sid, data); err != nil {
			return "error: failed to clear env vars", true
		}
		return "cleared all session environment variables", true

	default:
		return "unknown env command: " + parts[1], true
	}
}

// getSessionEnv retrieves environment variables for a session
func (te *TerminalEngine) getSessionEnv(ctx context.Context, sid string) map[string]string {
	kv, err := te.js.KeyValue(ctx, "sessions")
	if err != nil {
		return nil
	}

	var info SessionInfo
	if entry, err := kv.Get(ctx, sid); err == nil && entry != nil {
		_ = json.Unmarshal(entry.Value(), &info)
	}
	return info.Env
}

// SessionInfo extends the basic subscription list with env vars
type SessionInfo struct {
	Subscriptions []string          `json:"subscriptions"`
	Env           map[string]string `json:"env,omitempty"`
}

// ensureSessionSubscribed adds a subject to the session if not already present
func (te *TerminalEngine) ensureSessionSubscribed(ctx context.Context, sid, subject string) {
	kv, err := te.js.KeyValue(ctx, "sessions")
	if err != nil {
		return
	}

	var info SessionInfo
	if entry, err := kv.Get(ctx, sid); err == nil && entry != nil {
		_ = json.Unmarshal(entry.Value(), &info)
	}

	if !slices.Contains(info.Subscriptions, subject) {
		info.Subscriptions = append(info.Subscriptions, subject)
		slices.Sort(info.Subscriptions)
		if data, err := json.Marshal(info); err == nil {
			_, _ = kv.Put(ctx, sid, data)
		}
	}
}

// tokenizeCommand splits a command string respecting quoted strings
func tokenizeCommand(cmd string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range cmd {
		switch {
		case !inQuote && (r == '"' || r == '\''):
			inQuote = true
			quoteChar = r
		case inQuote && r == quoteChar:
			inQuote = false
			quoteChar = 0
		case !inQuote && r == ' ':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// fileExists is a helper to check if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
		info := SessionInfo{Subscriptions: subs}
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
