# Messaging Schema Consolidation - Design Document

## Objective

Consolidate all NATS messaging contracts into a centralized schema that provides:
- Single source of truth for all message types and subjects
- Type safety and validation for message construction
- Easy discoverability for developers and LLMs
- Backward compatibility with existing code patterns

## Current State Problems

1. **Message definitions scattered** across multiple files (terminal.go, script_runner.go, renderers.go)
2. **Subject patterns duplicated** and prone to typos (`"command.script.create"` strings everywhere)
3. **Unsafe message construction** using `map[string]any` with potential runtime errors
4. **No central documentation** of available commands/events
5. **Difficult onboarding** - new developers must hunt through codebase to understand messaging API

## Target Architecture

### File Structure
```
internal/messages/
├── schema.go    # All message type definitions and subject constants
├── builders.go  # Constructors, validators, and publishing helpers
└── doc.go       # Package documentation
```

### Design Principles

1. **Schema-First**: All message contracts defined in one place
2. **Type Safety**: Replace `map[string]any` with Go structs
3. **Developer Ergonomics**: Fluent builders for easy construction
4. **LLM Readable**: Clear naming and structure for AI comprehension
5. **Gradual Migration**: Can be adopted domain-by-domain

## Detailed Specification

### schema.go

**Purpose**: Single source of truth for all message contracts

**Contents**:
- Core interfaces (Message, Command, Event)
- All command type definitions
- All event type definitions  
- Subject pattern constants
- Basic method implementations (Subject(), IsCommand(), IsEvent())

**Structure**:
```go
package messages

import (
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
    // Script domain
    ScriptCreateSubject      = "command.script.create"
    ScriptRunSubject         = "command.script.*.run"         // * = script name
    ScriptCreatedSubject     = "event.script.*.created"       // * = script name
    ScriptJobStartedSubject  = "event.script.*.job.*.started" // script name, job id
    ScriptJobStdoutSubject   = "event.script.*.job.*.stdout"
    ScriptJobStderrSubject   = "event.script.*.job.*.stderr"
    ScriptJobExitSubject     = "event.script.*.job.*.exit"
    ScriptCreateErrorSubject = "event.script.*.create.error"
    
    // Terminal domain
    TerminalCommandSubject = "terminal.session.*.command"     // * = session id
    TerminalFreezeSubject  = "event.terminal.session.*.freeze"
    TerminalViewDocSubject = "event.terminal.session.*.viewdoc"
    
    // Add other domains as needed...
)

// =============================================================================
// SCRIPT DOMAIN
// =============================================================================

// ScriptCreateCommand requests creation of a new script project
type ScriptCreateCommand struct {
    ScriptName    string `json:"script_name"`
    ScriptType    string `json:"script_type"` // "python" | "typescript"
    CorrelationID string `json:"correlation_id,omitempty"`
}

func (c ScriptCreateCommand) Subject() string { return ScriptCreateSubject }
func (c ScriptCreateCommand) IsCommand()      {}
func (c ScriptCreateCommand) Validate() error {
    // Implementation in builders.go
    return validateScriptCreateCommand(c)
}

// ScriptRunCommand requests execution of an existing script
type ScriptRunCommand struct {
    ScriptName    string            `json:"-"` // Derived from subject
    Args          []string          `json:"args,omitempty"`
    Env           map[string]string `json:"env,omitempty"`
    CorrelationID string            `json:"correlation_id,omitempty"`
}

func (c ScriptRunCommand) Subject() string { 
    return fmt.Sprintf("command.script.%s.run", c.ScriptName)
}
func (c ScriptRunCommand) IsCommand() {}
func (c ScriptRunCommand) Validate() error {
    return validateScriptRunCommand(c)
}

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
func (e ScriptCreatedEvent) IsEvent() {}
func (e ScriptCreatedEvent) Timestamp() time.Time { return e.CreatedAt }
func (e ScriptCreatedEvent) Validate() error { return nil }

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
func (e ScriptJobStartedEvent) IsEvent() {}
func (e ScriptJobStartedEvent) Timestamp() time.Time { return e.StartedAt }
func (e ScriptJobStartedEvent) Validate() error { return nil }

// ... Continue pattern for all other script events ...

// =============================================================================
// TERMINAL DOMAIN
// =============================================================================

// TerminalCommandMessage represents a command entered in the terminal
type TerminalCommandMessage struct {
    SessionID string `json:"-"` // Derived from subject
    Cmd       string `json:"cmd"`
}

func (c TerminalCommandMessage) Subject() string {
    return fmt.Sprintf("terminal.session.%s.command", c.SessionID)
}
func (c TerminalCommandMessage) IsCommand() {}
func (c TerminalCommandMessage) Validate() error { return nil }

// TerminalFreezeEvent represents terminal output to be frozen/displayed
type TerminalFreezeEvent struct {
    SessionID string    `json:"session_id"`
    Cmd       string    `json:"cmd"`
    Output    string    `json:"output"`
    Timestamp time.Time `json:"timestamp"`
}

func (e TerminalFreezeEvent) Subject() string {
    return fmt.Sprintf("event.terminal.session.%s.freeze", e.SessionID)
}
func (e TerminalFreezeEvent) IsEvent() {}
func (e TerminalFreezeEvent) Timestamp() time.Time { return e.Timestamp }
func (e TerminalFreezeEvent) Validate() error { return nil }

// ... Continue for all other domains ...
```

### builders.go

**Purpose**: Provide ergonomic constructors, validation, and publishing helpers

**Contents**:
- Constructor functions for all message types
- Fluent builder methods
- Validation implementations
- Type-safe publisher wrapper

**Structure**:
```go
package messages

import (
    "context"
    "encoding/json"
    "fmt"
    "regexp"
    
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

// ... Continue pattern for all message types ...

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
    return nil
}

// ... Continue for all message types that need validation ...

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
        "script.create":      ScriptCreateSubject,
        "script.run":         ScriptRunSubject,
        "script.created":     ScriptCreatedSubject,
        "script.job.started": ScriptJobStartedSubject,
        "script.job.stdout":  ScriptJobStdoutSubject,
        "script.job.stderr":  ScriptJobStderrSubject,
        "script.job.exit":    ScriptJobExitSubject,
        "terminal.command":   TerminalCommandSubject,
        "terminal.freeze":    TerminalFreezeSubject,
        "terminal.viewdoc":   TerminalViewDocSubject,
    }
}
```

## Migration Strategy

### Phase 1: Create Schema Package
1. Create `internal/messages/` package
2. Implement `schema.go` with all current message types
3. Implement `builders.go` with constructors and publisher
4. Add tests for validation and message construction

### Phase 2: Migrate Script Domain
1. Update `internal/runtime/script_runner.go`:
   - Replace `map[string]any` with typed commands/events
   - Use `messages.Publisher` for type-safe publishing
   - Import subject constants from schema

### Phase 3: Migrate Terminal Domain  
1. Update `internal/runtime/terminal.go`:
   - Use typed command/event structures
   - Replace hardcoded subject strings with constants

### Phase 4: Migrate Renderers
1. Update `internal/runtime/renderers.go`:
   - Use subject pattern constants from schema
   - Update renderer specs to use typed events

### Phase 5: Migrate Remaining Code
1. Update any remaining files using messaging
2. Remove old hardcoded subject strings
3. Deprecate old helper functions

## Usage Examples

### Before (Current State)
```go
// In terminal.go - unsafe map construction
payload := map[string]any{
    "script_name":    name,
    "script_type":    lang,
    "correlation_id": in.Cmd,
}
_ = te.publishCommand(ctx, "command.script.create", payload)

// In script_runner.go - hardcoded subjects  
sr.publishEvent(
    fmt.Sprintf("event.script.%s.created", in.ScriptName),
    map[string]any{"correlation_id": in.CorrelationID},
    in.CorrelationID,
)
```

### After (Target State)
```go
// In terminal.go - type-safe construction
cmd := messages.NewScriptCreateCommand(name, lang).WithCorrelation(in.Cmd)
if err := publisher.PublishCommand(ctx, cmd); err != nil {
    slog.Error("Failed to publish script create command", "err", err)
    return
}

// In script_runner.go - type-safe event publishing
evt := messages.NewScriptCreatedEvent(in.ScriptName, in.ScriptType).WithCorrelation(in.CorrelationID)
if err := publisher.PublishEvent(ctx, evt); err != nil {
    slog.Error("Failed to publish script created event", "err", err)
}
```

## Implementation Requirements

### Must Have
1. **Backward Compatibility**: Existing code continues to work during migration
2. **Type Safety**: All message construction must be validated at compile time
3. **Single Source of Truth**: All subject patterns defined once in schema
4. **Easy Migration**: Each domain can be migrated independently

### Should Have  
1. **Good Error Messages**: Validation errors should be clear and actionable
2. **Builder Pattern**: Fluent API for message construction
3. **Performance**: No significant overhead compared to current approach

### Nice to Have
1. **Generate Documentation**: Could auto-generate docs from schema
2. **Generate CLI Examples**: Could auto-generate NATS CLI commands
3. **Schema Evolution**: Support for backward-compatible schema changes

## Success Metrics

1. **Single File Discovery**: All message types discoverable in schema.go
2. **Compile-Time Safety**: Zero runtime message construction errors
3. **Reduced Duplication**: No hardcoded subject strings outside schema
4. **Easy Onboarding**: New developers can understand messaging API from schema.go alone
5. **LLM Comprehension**: AI can understand full messaging API from schema.go

## Risks and Mitigation

### Risk: Breaking Changes During Migration
**Mitigation**: Keep old functions working alongside new ones until migration complete

### Risk: Performance Overhead
**Mitigation**: Benchmark message construction and publishing performance

### Risk: Over-Engineering  
**Mitigation**: Start simple, avoid adding features until proven necessary

### Risk: Schema Becomes Too Large
**Mitigation**: Consider splitting by domain if schema.go exceeds reasonable size (~1000 lines)

## Deliverables

1. `internal/messages/schema.go` - Complete message schema
2. `internal/messages/builders.go` - Constructors and utilities  
3. `internal/messages/doc.go` - Package documentation
4. Tests covering all message types and validation
5. Migration guide for updating existing code
6. Updated README with messaging API documentation