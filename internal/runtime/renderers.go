package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	components "binrun/ui/components"

	"github.com/nats-io/nats.go/jetstream"
	datastar "github.com/starfederation/datastar/sdk/go"
)

// ─────────────────── TERMINAL EVENTS ───────────────────

type TerminalEvent struct {
	Cmd    string `json:"cmd"`
	Output string `json:"output"`
}

func renderTerminal(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, evt TerminalEvent) error {
	// 1. append frozen line
	if err := sse.MergeFragmentTempl(
		components.TerminalFrozenLine(evt.Cmd, evt.Output),
		datastar.WithSelectorID("terminal-frozen"),
		datastar.WithMergeAppend(),
	); err != nil {
		return err
	}

	// 2. replace live prompt
	return sse.MergeFragmentTempl(
		components.TerminalPrompt(),
		datastar.WithSelectorID("live-prompt"),
	)
}

// ViewDoc event triggers markdown file rendering in left panel
type ViewDocEvent struct {
	Paths []string `json:"paths"`
}

func renderViewDoc(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, evt ViewDocEvent) error {
	slog.Info("renderViewDoc called", "subject", msg.Subject(), "receivedPaths", evt.Paths)
	// Check if paths were provided
	if len(evt.Paths) == 0 {
		// Maybe render an error or default content?
		// For now, let's assume the terminal command always sends at least one path.
		slog.Warn("renderViewDoc received empty paths, skipping fragment merge.")
		return nil // Or return an error
	}
	frag := components.DocMarkdown(evt.Paths)
	// The fragment now includes the target div, so no selector needed.
	return sse.MergeFragmentTempl(frag)
}

// ─────────────────── SCRIPT EVENTS ─────────────────────

// minimal helper components for now in ui/components

// Created ok
type ScriptCreated struct {
	CorrelationID string `json:"correlation_id"`
}

// parseScriptSubject extracts script name and optional job ID from subjects of
// the form:
//
//	event.script.<name>.created
//	event.script.<name>.create.error
//	event.script.<name>.job.<jobid>.<event>
//
// It returns empty strings if the expected tokens are not present.
func parseScriptSubject(subj string) (scriptName, jobID string) {
	parts := strings.Split(subj, ".")
	if len(parts) >= 3 {
		scriptName = parts[2]
	}
	if len(parts) >= 5 && parts[3] == "job" {
		jobID = parts[4]
	}
	return
}

func renderScriptCreated(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, _ ScriptCreated) error {
	scriptName, _ := parseScriptSubject(msg.Subject())
	frag := components.ScriptOutputLine(scriptName, "", "created", false)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// create error

type ScriptCreateErr struct {
	Error         string `json:"error"`
	CorrelationID string `json:"correlation_id"`
}

func renderScriptCreateErr(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, e ScriptCreateErr) error {
	scriptName, _ := parseScriptSubject(msg.Subject())
	frag := components.ScriptOutputLine(scriptName, "", "create error: "+e.Error, true)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// job started

type ScriptJobStarted struct {
	PID           int    `json:"pid"`
	CorrelationID string `json:"correlation_id"`
}

func renderJobStarted(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, j ScriptJobStarted) error {
	scriptName, jobID := parseScriptSubject(msg.Subject())
	frag := components.ScriptOutputLine(scriptName, jobID, fmt.Sprintf("job started pid=%d", j.PID), false)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// job output / stderr

type ScriptJobOutput struct {
	Data          string `json:"data"`
	CorrelationID string `json:"correlation_id"`
}

func renderJobOutput(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, j ScriptJobOutput) error {
	scriptName, jobID := parseScriptSubject(msg.Subject())
	isErr := strings.HasSuffix(msg.Subject(), ".stderr")
	frag := components.ScriptOutputLine(scriptName, jobID, j.Data, isErr)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// job exit

type ScriptJobExit struct {
	ExitCode      int    `json:"exit_code"`
	CorrelationID string `json:"correlation_id"`
}

func renderJobExit(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, e ScriptJobExit) error {
	scriptName, jobID := parseScriptSubject(msg.Subject())
	frag := components.ScriptOutputLine(scriptName, jobID, fmt.Sprintf("exit %d", e.ExitCode), false)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// ─────────────────── REGISTRY ──────────────────────────

func init() {
	Specs = []RendererSpec{
		// Doc viewer
		{Pattern: "event.terminal.session.*.viewdoc", Build: func(subj string) Renderer {
			return newTypedRenderer[ViewDocEvent](subj, renderViewDoc)
		}},
		{Pattern: "event.terminal.session.*.freeze", Build: func(subj string) Renderer {
			return newTypedRenderer[TerminalEvent](subj, renderTerminal)
		}},

		// script lifecycle
		{Pattern: "event.script.*.created", Build: func(subj string) Renderer { return newSubRenderer(subj, renderScriptCreated) }},
		{Pattern: "event.script.*.create.error", Build: func(subj string) Renderer { return newSubRenderer(subj, renderScriptCreateErr) }},
		{Pattern: "event.script.*.job.*.started", Build: func(subj string) Renderer { return newSubRenderer(subj, renderJobStarted) }},
		{Pattern: "event.script.*.job.*.stdout", Build: func(subj string) Renderer { return newSubRenderer(subj, renderJobOutput) }},
		{Pattern: "event.script.*.job.*.stderr", Build: func(subj string) Renderer { return newSubRenderer(subj, renderJobOutput) }},
		{Pattern: "event.script.*.job.*.exit", Build: func(subj string) Renderer { return newSubRenderer(subj, renderJobExit) }},
	}
}
