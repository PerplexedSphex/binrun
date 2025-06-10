package messages

import (
	"encoding/json"
	"fmt"
	"time"
)

// =============================================================================
// CORE INTERFACES
// =============================================================================

// Message represents any message in the system
type Message interface {
	Subject() string
	Validate() error
}

// Command represents an input that requests something to happen
type Command interface {
	Message
	IsCommand()
}

// Event represents something that has happened
type Event interface {
	Message
	IsEvent()
	Timestamp() time.Time
}

// =============================================================================
// SUBJECT CONSTANTS - Single source of truth for all subjects
// =============================================================================

const (
	// Script domain - Commands
	ScriptCreateSubject = "command.script.create"
	ScriptRunSubject    = "command.script.run" // Static subject

	// Script domain - Events
	ScriptCreatedSubjectPattern     = "event.script.*.created"       // * = script name
	ScriptCreateErrorSubjectPattern = "event.script.*.create.error"  // * = script name
	ScriptJobStartedSubjectPattern  = "event.script.*.job.*.started" // script name, job id
	ScriptJobStdoutSubjectPattern   = "event.script.*.job.*.stdout"
	ScriptJobStderrSubjectPattern   = "event.script.*.job.*.stderr"
	ScriptJobExitSubjectPattern     = "event.script.*.job.*.exit"
	ScriptJobErrorSubjectPattern    = "event.script.*.job.error"  // * = script name
	ScriptJobDataSubjectPattern     = "event.script.*.job.*.data" // NEW

	// Terminal domain
	TerminalCommandSubject       = "terminal.command" // Static subject
	TerminalFreezeSubjectPattern = "event.terminal.session.*.freeze"
)

// =============================================================================
// SCRIPT DOMAIN - COMMANDS
// =============================================================================

// ScriptCreateCommand requests creation of a new script project
type ScriptCreateCommand struct {
	ScriptName    string `json:"script_name" required:"true" placeholder:"my-script"`
	ScriptType    string `json:"script_type" required:"true" field_type:"select" options:"python,typescript"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

func (c ScriptCreateCommand) Subject() string { return ScriptCreateSubject }
func (c ScriptCreateCommand) IsCommand()      {}
func (c ScriptCreateCommand) Validate() error {
	return validateScriptCreateCommand(c)
}

// ScriptRunCommand requests execution of an existing script
type ScriptRunCommand struct {
	ScriptName    string            `json:"script_name"`
	Input         json.RawMessage   `json:"input"` // NEW
	Env           map[string]string `json:"env"`
	CorrelationID string            `json:"cid"`
}

func (c ScriptRunCommand) Subject() string { return ScriptRunSubject }
func (c ScriptRunCommand) IsCommand()      {}
func (c ScriptRunCommand) Validate() error {
	return validateScriptRunCommand(c)
}

// =============================================================================
// SCRIPT DOMAIN - EVENTS
// =============================================================================

// ScriptCreatedEvent indicates successful script creation
type ScriptCreatedEvent struct {
	ScriptName    string    `json:"script_name"`
	ScriptType    string    `json:"script_type"`
	CreatedAt     time.Time `json:"created_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func (e ScriptCreatedEvent) Subject() string {
	return fmt.Sprintf("event.script.%s.created", e.ScriptName)
}
func (e ScriptCreatedEvent) IsEvent()             {}
func (e ScriptCreatedEvent) Timestamp() time.Time { return e.CreatedAt }
func (e ScriptCreatedEvent) Validate() error      { return nil }

// ScriptCreateErrorEvent indicates script creation failure
type ScriptCreateErrorEvent struct {
	ScriptName    string    `json:"script_name"`
	Error         string    `json:"error"`
	OccurredAt    time.Time `json:"occurred_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func (e ScriptCreateErrorEvent) Subject() string {
	return fmt.Sprintf("event.script.%s.create.error", e.ScriptName)
}
func (e ScriptCreateErrorEvent) IsEvent()             {}
func (e ScriptCreateErrorEvent) Timestamp() time.Time { return e.OccurredAt }
func (e ScriptCreateErrorEvent) Validate() error      { return nil }

// ScriptJobStartedEvent indicates a script job has begun execution
type ScriptJobStartedEvent struct {
	ScriptName    string    `json:"script_name"`
	JobID         string    `json:"job_id"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"started_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func (e ScriptJobStartedEvent) Subject() string {
	return fmt.Sprintf("event.script.%s.job.%s.started", e.ScriptName, e.JobID)
}
func (e ScriptJobStartedEvent) IsEvent()             {}
func (e ScriptJobStartedEvent) Timestamp() time.Time { return e.StartedAt }
func (e ScriptJobStartedEvent) Validate() error      { return nil }

// ScriptJobOutputEvent represents stdout/stderr output from a running job
type ScriptJobOutputEvent struct {
	ScriptName    string    `json:"script_name"`
	JobID         string    `json:"job_id"`
	Stream        string    `json:"stream"` // "stdout" | "stderr"
	Data          string    `json:"data"`
	EmittedAt     time.Time `json:"emitted_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func (e ScriptJobOutputEvent) Subject() string {
	return fmt.Sprintf("event.script.%s.job.%s.%s", e.ScriptName, e.JobID, e.Stream)
}
func (e ScriptJobOutputEvent) IsEvent()             {}
func (e ScriptJobOutputEvent) Timestamp() time.Time { return e.EmittedAt }
func (e ScriptJobOutputEvent) Validate() error      { return nil }

// ScriptJobExitEvent indicates a script job has completed
type ScriptJobExitEvent struct {
	ScriptName    string    `json:"script_name"`
	JobID         string    `json:"job_id"`
	ExitCode      int       `json:"exit_code"`
	Error         string    `json:"error,omitempty"`
	ExitedAt      time.Time `json:"exited_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func (e ScriptJobExitEvent) Subject() string {
	return fmt.Sprintf("event.script.%s.job.%s.exit", e.ScriptName, e.JobID)
}
func (e ScriptJobExitEvent) IsEvent()             {}
func (e ScriptJobExitEvent) Timestamp() time.Time { return e.ExitedAt }
func (e ScriptJobExitEvent) Validate() error      { return nil }

// ScriptJobErrorEvent indicates a job failed to start
type ScriptJobErrorEvent struct {
	ScriptName    string    `json:"script_name"`
	Error         string    `json:"error"`
	OccurredAt    time.Time `json:"occurred_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func (e ScriptJobErrorEvent) Subject() string {
	return fmt.Sprintf("event.script.%s.job.error", e.ScriptName)
}
func (e ScriptJobErrorEvent) IsEvent()             {}
func (e ScriptJobErrorEvent) Timestamp() time.Time { return e.OccurredAt }
func (e ScriptJobErrorEvent) Validate() error      { return nil }

// ScriptJobDataEvent carries structured JSON emitted by a running job.
type ScriptJobDataEvent struct {
	ScriptName    string          `json:"script_name"`
	JobID         string          `json:"job_id"`
	Payload       json.RawMessage `json:"payload"`
	EmittedAt     time.Time       `json:"emitted_at"`
	CorrelationID string          `json:"correlation_id,omitempty"`
}

func (e ScriptJobDataEvent) Subject() string {
	return fmt.Sprintf("event.script.%s.job.%s.data", e.ScriptName, e.JobID)
}
func (e ScriptJobDataEvent) IsEvent()             {}
func (e ScriptJobDataEvent) Timestamp() time.Time { return e.EmittedAt }
func (e ScriptJobDataEvent) Validate() error      { return nil }

// =============================================================================
// TERMINAL DOMAIN
// =============================================================================

// TerminalCommandMessage represents a command entered in the terminal
type TerminalCommandMessage struct {
	SessionID string `json:"session_id" required:"true"`
	Cmd       string `json:"cmd" required:"true"`
}

func (c TerminalCommandMessage) Subject() string { return TerminalCommandSubject }
func (c TerminalCommandMessage) IsCommand()      {}
func (c TerminalCommandMessage) Validate() error {
	if c.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if c.Cmd == "" {
		return fmt.Errorf("cmd is required")
	}
	return nil
}

// TerminalFreezeEvent represents terminal output to be frozen/displayed
type TerminalFreezeEvent struct {
	SessionID string    `json:"session_id"`
	Cmd       string    `json:"cmd"`
	Output    string    `json:"output"`
	FrozenAt  time.Time `json:"frozen_at"`
}

func (e TerminalFreezeEvent) Subject() string {
	return fmt.Sprintf("event.terminal.session.%s.freeze", e.SessionID)
}
func (e TerminalFreezeEvent) IsEvent()             {}
func (e TerminalFreezeEvent) Timestamp() time.Time { return e.FrozenAt }
func (e TerminalFreezeEvent) Validate() error      { return nil }

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// Subject builder functions for dynamic subjects (events only)
func ScriptCreatedSubject(scriptName string) string {
	return fmt.Sprintf("event.script.%s.created", scriptName)
}

func ScriptCreateErrorSubject(scriptName string) string {
	return fmt.Sprintf("event.script.%s.create.error", scriptName)
}

func ScriptJobStartedSubject(scriptName, jobID string) string {
	return fmt.Sprintf("event.script.%s.job.%s.started", scriptName, jobID)
}

func ScriptJobStdoutSubject(scriptName, jobID string) string {
	return fmt.Sprintf("event.script.%s.job.%s.stdout", scriptName, jobID)
}

func ScriptJobStderrSubject(scriptName, jobID string) string {
	return fmt.Sprintf("event.script.%s.job.%s.stderr", scriptName, jobID)
}

func ScriptJobExitSubject(scriptName, jobID string) string {
	return fmt.Sprintf("event.script.%s.job.%s.exit", scriptName, jobID)
}

func ScriptJobErrorSubject(scriptName string) string {
	return fmt.Sprintf("event.script.%s.job.error", scriptName)
}

func ScriptJobDataSubject(scriptName, jobID string) string {
	return fmt.Sprintf("event.script.%s.job.%s.data", scriptName, jobID)
}

func TerminalFreezeSubject(sessionID string) string {
	return fmt.Sprintf("event.terminal.session.%s.freeze", sessionID)
}
