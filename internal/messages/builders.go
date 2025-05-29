package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// =============================================================================
// CONSTRUCTORS - Easy message creation
// =============================================================================

// NewScriptCreateCommand creates a script creation command
func NewScriptCreateCommand(name, scriptType string) *ScriptCreateCommand {
	return &ScriptCreateCommand{
		ScriptName: name,
		ScriptType: scriptType,
	}
}

// WithCorrelation adds correlation ID to script create command
func (c *ScriptCreateCommand) WithCorrelation(id string) *ScriptCreateCommand {
	c.CorrelationID = id
	return c
}

// NewScriptRunCommand creates a script execution command
func NewScriptRunCommand(name string) *ScriptRunCommand {
	return &ScriptRunCommand{ScriptName: name}
}

// WithArgs adds arguments to script run command
func (c *ScriptRunCommand) WithArgs(args ...string) *ScriptRunCommand {
	c.Args = args
	return c
}

// WithEnv adds environment variables to script run command
func (c *ScriptRunCommand) WithEnv(env map[string]string) *ScriptRunCommand {
	c.Env = env
	return c
}

// WithCorrelation adds correlation ID to script run command
func (c *ScriptRunCommand) WithCorrelation(id string) *ScriptRunCommand {
	c.CorrelationID = id
	return c
}

// NewScriptCreatedEvent creates a script created event
func NewScriptCreatedEvent(name, scriptType string) *ScriptCreatedEvent {
	return &ScriptCreatedEvent{
		ScriptName: name,
		ScriptType: scriptType,
		CreatedAt:  time.Now(),
	}
}

// WithCorrelation adds correlation ID to script created event
func (e *ScriptCreatedEvent) WithCorrelation(id string) *ScriptCreatedEvent {
	e.CorrelationID = id
	return e
}

// NewScriptCreateErrorEvent creates a script creation error event
func NewScriptCreateErrorEvent(name, errorMsg string) *ScriptCreateErrorEvent {
	return &ScriptCreateErrorEvent{
		ScriptName: name,
		Error:      errorMsg,
		OccurredAt: time.Now(),
	}
}

// WithCorrelation adds correlation ID to script create error event
func (e *ScriptCreateErrorEvent) WithCorrelation(id string) *ScriptCreateErrorEvent {
	e.CorrelationID = id
	return e
}

// NewScriptJobStartedEvent creates a job started event
func NewScriptJobStartedEvent(scriptName, jobID string, pid int) *ScriptJobStartedEvent {
	return &ScriptJobStartedEvent{
		ScriptName: scriptName,
		JobID:      jobID,
		PID:        pid,
		StartedAt:  time.Now(),
	}
}

// WithCorrelation adds correlation ID to job started event
func (e *ScriptJobStartedEvent) WithCorrelation(id string) *ScriptJobStartedEvent {
	e.CorrelationID = id
	return e
}

// NewScriptJobOutputEvent creates a job output event
func NewScriptJobOutputEvent(scriptName, jobID, stream, data string) *ScriptJobOutputEvent {
	return &ScriptJobOutputEvent{
		ScriptName: scriptName,
		JobID:      jobID,
		Stream:     stream,
		Data:       data,
		EmittedAt:  time.Now(),
	}
}

// WithCorrelation adds correlation ID to job output event
func (e *ScriptJobOutputEvent) WithCorrelation(id string) *ScriptJobOutputEvent {
	e.CorrelationID = id
	return e
}

// NewScriptJobExitEvent creates a job exit event
func NewScriptJobExitEvent(scriptName, jobID string, exitCode int) *ScriptJobExitEvent {
	return &ScriptJobExitEvent{
		ScriptName: scriptName,
		JobID:      jobID,
		ExitCode:   exitCode,
		ExitedAt:   time.Now(),
	}
}

// WithError adds error message to job exit event
func (e *ScriptJobExitEvent) WithError(err string) *ScriptJobExitEvent {
	e.Error = err
	return e
}

// WithCorrelation adds correlation ID to job exit event
func (e *ScriptJobExitEvent) WithCorrelation(id string) *ScriptJobExitEvent {
	e.CorrelationID = id
	return e
}

// NewScriptJobErrorEvent creates a job error event
func NewScriptJobErrorEvent(scriptName, errorMsg string) *ScriptJobErrorEvent {
	return &ScriptJobErrorEvent{
		ScriptName: scriptName,
		Error:      errorMsg,
		OccurredAt: time.Now(),
	}
}

// WithCorrelation adds correlation ID to job error event
func (e *ScriptJobErrorEvent) WithCorrelation(id string) *ScriptJobErrorEvent {
	e.CorrelationID = id
	return e
}

// NewTerminalCommandMessage creates a terminal command message
func NewTerminalCommandMessage(sessionID, cmd string) *TerminalCommandMessage {
	return &TerminalCommandMessage{
		SessionID: sessionID,
		Cmd:       cmd,
	}
}

// NewTerminalFreezeEvent creates a terminal freeze event
func NewTerminalFreezeEvent(sessionID, cmd, output string) *TerminalFreezeEvent {
	return &TerminalFreezeEvent{
		SessionID: sessionID,
		Cmd:       cmd,
		Output:    output,
		FrozenAt:  time.Now(),
	}
}

// NewTerminalViewDocEvent creates a terminal view doc event
func NewTerminalViewDocEvent(sessionID string, paths []string) *TerminalViewDocEvent {
	return &TerminalViewDocEvent{
		SessionID: sessionID,
		Paths:     paths,
		ViewedAt:  time.Now(),
	}
}

// =============================================================================
// VALIDATION - Implementation of Validate() methods
// =============================================================================

var scriptNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateScriptCreateCommand implements validation for ScriptCreateCommand
func validateScriptCreateCommand(c ScriptCreateCommand) error {
	if c.ScriptName == "" {
		return fmt.Errorf("script_name is required")
	}
	if !scriptNameRegex.MatchString(c.ScriptName) {
		return fmt.Errorf("script_name must contain only alphanumeric characters, hyphens, and underscores")
	}
	if c.ScriptType != "python" && c.ScriptType != "typescript" {
		return fmt.Errorf("script_type must be 'python' or 'typescript'")
	}
	return nil
}

// validateScriptRunCommand implements validation for ScriptRunCommand
func validateScriptRunCommand(c ScriptRunCommand) error {
	if c.ScriptName == "" {
		return fmt.Errorf("script_name is required")
	}
	if !scriptNameRegex.MatchString(c.ScriptName) {
		return fmt.Errorf("script_name must contain only alphanumeric characters, hyphens, and underscores")
	}
	return nil
}

// =============================================================================
// PUBLISHER - Type-safe message publishing
// =============================================================================

// Publisher provides type-safe message publishing
type Publisher struct {
	js jetstream.JetStream
}

// NewPublisher creates a new type-safe publisher
func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

// PublishCommand publishes a command with validation
func (p *Publisher) PublishCommand(ctx context.Context, cmd Command) error {
	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("command validation failed: %w", err)
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}

	_, err = p.js.Publish(ctx, cmd.Subject(), data)
	if err != nil {
		return fmt.Errorf("publish command: %w", err)
	}

	return nil
}

// PublishEvent publishes an event with validation
func (p *Publisher) PublishEvent(ctx context.Context, evt Event) error {
	if err := evt.Validate(); err != nil {
		return fmt.Errorf("event validation failed: %w", err)
	}

	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	_, err = p.js.Publish(ctx, evt.Subject(), data)
	if err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	return nil
}

// =============================================================================
// UTILITIES - Helper functions for common operations
// =============================================================================

// SubjectPatterns returns all known subject patterns for renderer registration
func SubjectPatterns() map[string]string {
	return map[string]string{
		"script.create":       ScriptCreateSubject,
		"script.run":          ScriptRunSubject,
		"script.created":      ScriptCreatedSubjectPattern,
		"script.create.error": ScriptCreateErrorSubjectPattern,
		"script.job.started":  ScriptJobStartedSubjectPattern,
		"script.job.stdout":   ScriptJobStdoutSubjectPattern,
		"script.job.stderr":   ScriptJobStderrSubjectPattern,
		"script.job.exit":     ScriptJobExitSubjectPattern,
		"script.job.error":    ScriptJobErrorSubjectPattern,
		"terminal.command":    TerminalCommandSubject,
		"terminal.freeze":     TerminalFreezeSubjectPattern,
		"terminal.viewdoc":    TerminalViewDocSubjectPattern,
	}
}

// BuildCommand creates a typed command from UI form data
func BuildCommand(messageType string, data map[string]any) (Command, error) {
	switch messageType {
	case "ScriptCreateCommand":
		scriptName, _ := data["script_name"].(string)
		scriptType, _ := data["script_type"].(string)
		cmd := NewScriptCreateCommand(scriptName, scriptType)
		if corrID, ok := data["correlation_id"].(string); ok && corrID != "" {
			cmd.CorrelationID = corrID
		}
		return cmd, nil

	case "ScriptRunCommand":
		scriptName, _ := data["script_name"].(string)
		cmd := NewScriptRunCommand(scriptName)

		// Handle args - could be a single string or an array
		if args, ok := data["args"].([]any); ok {
			strArgs := make([]string, len(args))
			for i, v := range args {
				strArgs[i] = fmt.Sprint(v)
			}
			cmd = cmd.WithArgs(strArgs...)
		} else if argStr, ok := data["args"].(string); ok && argStr != "" {
			// Parse space-separated args for simple case
			cmd = cmd.WithArgs(strings.Fields(argStr)...)
		}

		// Handle env - expecting key=value pairs
		if envData, ok := data["env"].(map[string]any); ok {
			envMap := make(map[string]string)
			for k, v := range envData {
				envMap[k] = fmt.Sprint(v)
			}
			cmd = cmd.WithEnv(envMap)
		}

		if corrID, ok := data["correlation_id"].(string); ok && corrID != "" {
			cmd.CorrelationID = corrID
		}
		return cmd, nil

	case "TerminalCommandMessage":
		sessionID, _ := data["session_id"].(string)
		cmdText, _ := data["cmd"].(string)
		return NewTerminalCommandMessage(sessionID, cmdText), nil

	default:
		return nil, fmt.Errorf("unknown command type: %s", messageType)
	}
}

// GetCommandTypes returns all available command message types
func GetCommandTypes() []string {
	return []string{
		"ScriptCreateCommand",
		"ScriptRunCommand",
		"TerminalCommandMessage",
	}
}
