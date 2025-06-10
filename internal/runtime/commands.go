package runtime

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"binrun/internal/layout"
	"binrun/internal/messages"
)

// EchoCommand repeats the provided text back to the user.
// Usage: echo <text>
type EchoCommand struct{ engine *TerminalEngine }

func (c *EchoCommand) Name() string { return "echo" }
func (c *EchoCommand) Help() string { return "echo <text> - echo text back" }
func (c *EchoCommand) Execute(ctx context.Context, sessionID string, state layout.SessionState, args []string) (layout.SessionState, CommandResult) {
	out := ""
	if len(args) > 1 {
		out = strings.Join(args[1:], " ")
	}
	return state, CommandResult{Output: out}
}

// EnvCommand manages session environment variables.
// Usage: env set KEY=VALUE | env list | env clear
type EnvCommand struct{ engine *TerminalEngine }

func (c *EnvCommand) Name() string { return "env" }
func (c *EnvCommand) Help() string { return c.engine.helpText("env") }
func (c *EnvCommand) Execute(ctx context.Context, sessionID string, state layout.SessionState, args []string) (layout.SessionState, CommandResult) {
	if len(args) < 2 {
		return state, CommandResult{Output: "usage: env [set|list|clear]"}
	}
	sub := args[1]
	switch sub {
	case "set":
		if len(args) < 3 || !strings.Contains(args[2], "=") {
			return state, CommandResult{Output: "usage: env set KEY=VALUE"}
		}
		parts := strings.SplitN(strings.Join(args[2:], " "), "=", 2)
		key, val := parts[0], parts[1]
		if state.Env == nil {
			state.Env = make(map[string]string)
		}
		state.Env[key] = val
		return state, CommandResult{Output: fmt.Sprintf("set %s=%s", key, val)}
	case "list":
		if state.Env == nil || len(state.Env) == 0 {
			return state, CommandResult{Output: "no session environment variables set"}
		}
		keys := make([]string, 0, len(state.Env))
		for k := range state.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		lines := make([]string, len(keys))
		for i, k := range keys {
			lines[i] = fmt.Sprintf("%s=%s", k, state.Env[k])
		}
		return state, CommandResult{Output: "session environment:\n  " + strings.Join(lines, "\n  ")}
	case "clear":
		state.Env = nil
		return state, CommandResult{Output: "cleared all session environment variables"}
	default:
		return state, CommandResult{Output: "unknown env command: " + sub}
	}
}

// LSCommand lists scripts or presets.
// Usage: ls scripts | ls presets | ls preset <id>
type LSCommand struct{ engine *TerminalEngine }

func (c *LSCommand) Name() string { return "ls" }
func (c *LSCommand) Help() string { return c.engine.helpText("ls") }
func (c *LSCommand) Execute(ctx context.Context, sessionID string, state layout.SessionState, args []string) (layout.SessionState, CommandResult) {
	if len(args) < 2 {
		return state, CommandResult{Output: "usage: ls [scripts|presets|preset <id>]"}
	}
	switch args[1] {
	case "scripts":
		dirs, err := os.ReadDir("./scripts")
		if err != nil {
			return state, CommandResult{Output: "error reading scripts directory"}
		}
		list := []string{}
		for _, e := range dirs {
			if e.IsDir() {
				list = append(list, e.Name())
			}
		}
		sort.Strings(list)
		return state, CommandResult{Output: "available scripts:\n  " + strings.Join(list, "\n  ")}
	case "presets":
		keys := make([]string, 0, len(layout.Presets))
		for k := range layout.Presets {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return state, CommandResult{Output: "available presets: " + strings.Join(keys, ", ")}
	case "preset":
		if len(args) < 3 {
			return state, CommandResult{Output: "usage: ls preset <id>"}
		}
		id := args[2]
		p, ok := layout.Presets[id]
		if !ok {
			return state, CommandResult{Output: "unknown preset"}
		}
		subs := p.Build(map[string]string{})
		return state, CommandResult{Output: fmt.Sprintf("%s: %v", id, subs)}
	default:
		return state, CommandResult{Output: "unknown ls command"}
	}
}

// LoadCommand applies a preset to update the layout.
// Usage: load <presetID> [--key value ...]
type LoadCommand struct{ engine *TerminalEngine }

func (c *LoadCommand) Name() string { return "load" }
func (c *LoadCommand) Help() string { return c.engine.helpText("load") }
func (c *LoadCommand) Execute(ctx context.Context, sessionID string, state layout.SessionState, args []string) (layout.SessionState, CommandResult) {
	if len(args) < 2 {
		return state, CommandResult{Output: "usage: load <presetID> [--key value ...]"}
	}
	id := args[1]
	p, ok := layout.Presets[id]
	if !ok {
		return state, CommandResult{Output: "unknown preset"}
	}
	// parse flags after preset ID
	flagArgs, _ := parseFlags(args[2:])
	built, err := p.BuildLayout(flagArgs)
	if err != nil {
		return state, CommandResult{Output: fmt.Sprintf("error: %v", err)}
	}
	state.Layout = built
	// Persist state
	_ = c.engine.saveSessionState(ctx, sessionID, state)
	subs := built.GetRequiredSubscriptions(sessionID)
	return state, CommandResult{Output: fmt.Sprintf("preset %s loaded (%d subscriptions)", id, len(subs))}
}

// ScriptCommand handles script create, run, and info.
// Usage: script create|run|info <args>
type ScriptCommand struct{ engine *TerminalEngine }

func (c *ScriptCommand) Name() string { return "script" }
func (c *ScriptCommand) Help() string { return c.engine.helpText("script") }
func (c *ScriptCommand) Execute(ctx context.Context, sessionID string, state layout.SessionState, args []string) (layout.SessionState, CommandResult) {
	if len(args) < 2 {
		return state, CommandResult{Output: "usage: script [create|run|info] <args>"}
	}
	sub := args[1]
	switch sub {
	case "create":
		if len(args) < 4 {
			return state, CommandResult{Output: "usage: script create <name> <lang>"}
		}
		name, lang := args[2], args[3]
		cmd := messages.NewScriptCreateCommand(name, lang).WithCorrelation(strings.Join(args, " "))
		if err := c.engine.publisher.PublishCommand(ctx, cmd); err != nil {
			return state, CommandResult{Output: "error: failed to create script"}
		}
		return state, CommandResult{Output: "script create requested"}
	case "run":
		out := c.engine.handleScriptRun(ctx, sessionID, strings.Join(args, " "))
		return state, CommandResult{Output: out}
	case "info":
		if len(args) < 3 {
			return state, CommandResult{Output: "usage: script info <name>"}
		}
		out := c.engine.getScriptInfo(args[2])
		return state, CommandResult{Output: out}
	default:
		return state, CommandResult{Output: "error: unknown script command"}
	}
}

// ViewCommand shows one or more document files in the left panel.
// Usage: view <path> [path...]
type ViewCommand struct{ engine *TerminalEngine }

func (c *ViewCommand) Name() string { return "view" }
func (c *ViewCommand) Help() string { return c.engine.helpText("view") }
func (c *ViewCommand) Execute(ctx context.Context, sessionID string, state layout.SessionState, args []string) (layout.SessionState, CommandResult) {
	if len(args) < 2 {
		return state, CommandResult{Output: "usage: view <file> [file...]"}
	}
	// Ensure layout exists
	if state.Layout == nil || state.Layout.Panels == nil {
		state.Layout = &layout.PanelLayout{Panels: map[string]*layout.LayoutNode{}}
	}
	paths := args[1:]
	// Set the left panel to a document node
	state.Layout.Panels["left"] = &layout.LayoutNode{DocumentPaths: paths}
	return state, CommandResult{Output: fmt.Sprintf("opening %d files", len(paths))}
}
