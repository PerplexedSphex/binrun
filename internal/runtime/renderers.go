package runtime

import (
	"context"
	"fmt"
	"strings"

	"binrun/internal/messages"
	components "binrun/ui/components"

	"github.com/nats-io/nats.go/jetstream"
	datastar "github.com/starfederation/datastar/sdk/go"
)

// ─────────────────── TERMINAL EVENTS ───────────────────

func renderTerminal(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, evt messages.TerminalFreezeEvent) error {
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

// ─────────────────── SCRIPT EVENTS ─────────────────────

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

func renderScriptCreated(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, _ messages.ScriptCreatedEvent) error {
	scriptName, _ := parseScriptSubject(msg.Subject())
	frag := components.ScriptOutputLine(scriptName, "", "created", false)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

func renderScriptCreateErr(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, e messages.ScriptCreateErrorEvent) error {
	scriptName, _ := parseScriptSubject(msg.Subject())
	frag := components.ScriptOutputLine(scriptName, "", "create error: "+e.Error, true)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

func renderJobStarted(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, j messages.ScriptJobStartedEvent) error {
	scriptName, jobID := parseScriptSubject(msg.Subject())
	frag := components.ScriptOutputLine(scriptName, jobID, fmt.Sprintf("job started pid=%d", j.PID), false)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

func renderJobOutput(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, j messages.ScriptJobOutputEvent) error {
	scriptName, jobID := parseScriptSubject(msg.Subject())
	isErr := strings.HasSuffix(msg.Subject(), ".stderr")
	frag := components.ScriptOutputLine(scriptName, jobID, j.Data, isErr)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

func renderJobExit(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, selector string, e messages.ScriptJobExitEvent) error {
	scriptName, jobID := parseScriptSubject(msg.Subject())
	frag := components.ScriptOutputLine(scriptName, jobID, fmt.Sprintf("exit %d", e.ExitCode), false)
	return sse.MergeFragmentTempl(frag, datastar.WithSelector(selector), datastar.WithMergeAppend())
}

// ─────────────────── REGISTRY ──────────────────────────

func init() {
	Specs = []RendererSpec{
		{Pattern: messages.TerminalFreezeSubjectPattern, Build: func(subj string) Renderer {
			return newTypedRenderer[messages.TerminalFreezeEvent](subj, renderTerminal)
		}},

		// script lifecycle
		{Pattern: messages.ScriptCreatedSubjectPattern, Build: func(subj string) Renderer { return newSubRenderer(subj, renderScriptCreated) }},
		{Pattern: messages.ScriptCreateErrorSubjectPattern, Build: func(subj string) Renderer { return newSubRenderer(subj, renderScriptCreateErr) }},
		{Pattern: messages.ScriptJobStartedSubjectPattern, Build: func(subj string) Renderer { return newSubRenderer(subj, renderJobStarted) }},
		{Pattern: messages.ScriptJobStdoutSubjectPattern, Build: func(subj string) Renderer { return newSubRenderer(subj, renderJobOutput) }},
		{Pattern: messages.ScriptJobStderrSubjectPattern, Build: func(subj string) Renderer { return newSubRenderer(subj, renderJobOutput) }},
		{Pattern: messages.ScriptJobExitSubjectPattern, Build: func(subj string) Renderer { return newSubRenderer(subj, renderJobExit) }},
	}
}
