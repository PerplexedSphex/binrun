package core

import (
	"context"
	"fmt"

	components "binrun/ui/components"

	"github.com/nats-io/nats.go/jetstream"
	datastar "github.com/starfederation/datastar/sdk/go"
)

// ─────────────────── TERMINAL EVENTS ───────────────────

type TerminalEvent struct {
	LineID string `json:"line_id"`
	Cmd    string `json:"cmd"`
	Output string `json:"output"`
}

func renderTerminal(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, evt TerminalEvent) error {
	// Replace prompt line with frozen echo + response
	if err := sse.MergeFragmentTempl(
		components.TerminalFrozenLine(evt.Cmd, evt.Output),
		datastar.WithSelectorID("term-"+evt.LineID),
	); err != nil {
		return err
	}

	// Append next prompt (increment numeric part after leading 'L')
	next := 1
	if len(evt.LineID) > 1 {
		fmt.Sscanf(evt.LineID[1:], "%d", &next)
		next++
	}
	nextID := fmt.Sprintf("L%d", next)
	return sse.MergeFragmentTempl(
		components.TerminalPrompt(nextID),
		datastar.WithSelectorID("terminal-lines"),
		datastar.WithMergeAppend(),
	)
}

// ─────────────────── SCRIPT EVENTS ─────────────────────

// minimal helper components for now in ui/components

// Created ok
type ScriptCreated struct {
	CorrelationID string `json:"correlation_id"`
}

func renderScriptCreated(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, _ ScriptCreated) error {
	frag := components.ScriptStatus("created")
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// create error

type ScriptCreateErr struct {
	Error         string `json:"error"`
	CorrelationID string `json:"correlation_id"`
}

func renderScriptCreateErr(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, e ScriptCreateErr) error {
	frag := components.ScriptStatus("create error: " + e.Error)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// job started

type ScriptJobStarted struct {
	PID           int    `json:"pid"`
	CorrelationID string `json:"correlation_id"`
}

func renderJobStarted(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, j ScriptJobStarted) error {
	frag := components.ScriptStatus(fmt.Sprintf("job started pid=%d", j.PID))
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// job output / stderr

type ScriptJobOutput struct {
	Data          string `json:"data"`
	CorrelationID string `json:"correlation_id"`
}

func renderJobOutput(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, j ScriptJobOutput) error {
	frag := components.ScriptOutput(j.Data)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// job exit

type ScriptJobExit struct {
	ExitCode      int    `json:"exit_code"`
	CorrelationID string `json:"correlation_id"`
}

func renderJobExit(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, e ScriptJobExit) error {
	frag := components.ScriptStatus(fmt.Sprintf("exit %d", e.ExitCode))
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// ─────────────────── REGISTRY ──────────────────────────

var Renderers = []renderer{
	// terminal
	newSubRenderer[TerminalEvent]("terminal.session.*.event", renderTerminal),

	// script lifecycle
	newSubRenderer[ScriptCreated]("event.script.*.created", renderScriptCreated),
	newSubRenderer[ScriptCreateErr]("event.script.*.create.error", renderScriptCreateErr),

	newSubRenderer[ScriptJobStarted]("event.script.*.job.*.started", renderJobStarted),
	newSubRenderer[ScriptJobOutput]("event.script.*.job.*.stdout", renderJobOutput),
	newSubRenderer[ScriptJobOutput]("event.script.*.job.*.stderr", renderJobOutput),
	newSubRenderer[ScriptJobExit]("event.script.*.job.*.exit", renderJobExit),

	fallback, // last
}
