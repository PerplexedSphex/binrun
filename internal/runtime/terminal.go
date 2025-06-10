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

	layoutpkg "binrun/internal/layout"
	"binrun/internal/messages"

	"github.com/nats-io/nats.go/jetstream"
)

// TerminalEngine interprets terminal.session.*.command messages.
// It publishes terse feedback events to event.terminal.session.*.freeze.
// Any side-effecting commands (script create/run) are forwarded to the existing COMMAND stream subjects.

type TerminalEngine struct {
	js        jetstream.JetStream
	publisher *messages.Publisher
	commands  map[string]TerminalCommand
}

func NewTerminalEngine(js jetstream.JetStream) *TerminalEngine {
	engine := &TerminalEngine{
		js:        js,
		publisher: messages.NewPublisher(js),
		commands:  make(map[string]TerminalCommand),
	}
	// Register built-in commands
	engine.commands["help"] = &HelpCommand{engine: engine}
	engine.commands["echo"] = &EchoCommand{engine: engine}
	engine.commands["env"] = &EnvCommand{engine: engine}
	engine.commands["ls"] = &LSCommand{engine: engine}
	engine.commands["load"] = &LoadCommand{engine: engine}
	engine.commands["script"] = &ScriptCommand{engine: engine}
	engine.commands["view"] = &ViewCommand{engine: engine}
	return engine
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

// TerminalEngine.handleCommand dispatches incoming terminal commands via the command registry.
func (te *TerminalEngine) handleCommand(ctx context.Context, msg jetstream.Msg) {
	// Decode the incoming command message
	var in messages.TerminalCommandMessage
	if err := json.Unmarshal(msg.Data(), &in); err != nil {
		slog.Warn("terminal: bad cmd payload", "err", err)
		_ = msg.Ack()
		return
	}
	sid := in.SessionID
	if sid == "" {
		slog.Warn("terminal: missing session ID in message body")
		_ = msg.Ack()
		return
	}
	// Split command into parts
	parts := tokenizeCommand(in.Cmd)
	// Load current session state (or default)
	state, err := te.getSessionState(ctx, sid)
	if err != nil {
		state = layoutpkg.SessionState{Env: make(map[string]string), Layout: nil}
	}
	var newState layoutpkg.SessionState
	var result CommandResult
	// Dispatch
	if len(parts) > 0 {
		if cmd, ok := te.commands[parts[0]]; ok {
			newState, result = cmd.Execute(ctx, sid, state, parts)
		} else {
			newState, result = state, CommandResult{Output: "error: unknown command"}
		}
	} else {
		newState, result = state, CommandResult{Output: "error: empty command"}
	}
	// Persist new state
	_ = te.saveSessionState(ctx, sid, newState)
	// Send output back to the client
	te.sendFreeze(ctx, sid, in.Cmd, result.Output, msg)
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

		// Load session state to preserve subscriptions and layout
		entry, err := kv.Get(ctx, sid)
		state := layoutpkg.SessionState{}
		if err == nil && entry != nil {
			st, err2 := layoutpkg.LoadSessionData(entry.Value())
			if err2 == nil {
				state = st
			}
		}
		if state.Env == nil {
			state.Env = make(map[string]string)
		}
		state.Env[key] = value
		// Save back
		dataObj, _ := state.Raw()
		raw, _ := json.Marshal(dataObj)
		if _, err := kv.Put(ctx, sid, raw); err != nil {
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
		// Load session state
		entry, err := kv.Get(ctx, sid)
		state := layoutpkg.SessionState{}
		if err == nil && entry != nil {
			st, err2 := layoutpkg.LoadSessionData(entry.Value())
			if err2 == nil {
				state = st
			}
		}
		state.Env = nil
		// Save back
		dataObj, _ := state.Raw()
		raw, _ := json.Marshal(dataObj)
		if _, err := kv.Put(ctx, sid, raw); err != nil {
			return "error: failed to clear env vars", true
		}
		return "cleared all session environment variables", true

	default:
		return "unknown env command: " + parts[1], true
	}
}

// getSessionEnv retrieves environment variables for a session
func (te *TerminalEngine) getSessionEnv(ctx context.Context, sid string) map[string]string {
	// Access sessions KV
	kv, err := te.js.KeyValue(ctx, "sessions")
	if err != nil {
		return nil
	}
	// Fetch session data
	entry, err := kv.Get(ctx, sid)
	if err != nil || entry == nil {
		return nil
	}
	// Load unified session state
	state, err := layoutpkg.LoadSessionData(entry.Value())
	if err != nil {
		return nil
	}
	return state.Env
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

// HelpCommand is the built-in "help" terminal command.
type HelpCommand struct {
	engine *TerminalEngine
}

// Name returns the command key.
func (c *HelpCommand) Name() string { return "help" }

// Help returns the help text for the help command.
func (c *HelpCommand) Help() string { return c.engine.helpText("") }

// Execute runs the help command, returning unchanged state and help output.
func (c *HelpCommand) Execute(ctx context.Context, sessionID string, state layoutpkg.SessionState, args []string) (layoutpkg.SessionState, CommandResult) {
	topic := ""
	if len(args) > 1 {
		topic = args[1]
	}
	return state, CommandResult{Output: c.engine.helpText(topic)}
}

// getSessionState loads the session state from KV or returns an error.
func (te *TerminalEngine) getSessionState(ctx context.Context, sid string) (layoutpkg.SessionState, error) {
	kv, err := te.js.KeyValue(ctx, "sessions")
	if err != nil {
		return layoutpkg.SessionState{}, err
	}
	entry, err := kv.Get(ctx, sid)
	if err != nil {
		return layoutpkg.SessionState{}, err
	}
	return layoutpkg.LoadSessionData(entry.Value())
}

// saveSessionState persists the session state to KV (ignoring errors).
func (te *TerminalEngine) saveSessionState(ctx context.Context, sid string, state layoutpkg.SessionState) error {
	kv, err := te.js.KeyValue(ctx, "sessions")
	if err != nil {
		return err
	}
	dataObj, err := state.Raw()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(dataObj)
	if err != nil {
		return err
	}
	_, err = kv.Put(ctx, sid, raw)
	return err
}
