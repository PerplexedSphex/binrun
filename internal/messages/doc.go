// Package messages provides a centralized schema for all NATS messaging contracts.
//
// This package consolidates all message types, subject patterns, and validation logic
// into a single source of truth, providing:
//
//   - Type-safe message construction with compile-time validation
//   - Fluent builder pattern for ergonomic message creation
//   - Centralized subject constants to eliminate hardcoded strings
//   - Validation methods to ensure message integrity
//   - Type-safe publisher for command and event publishing
//
// # Message Types
//
// The package defines two main categories of messages:
//
//   - Commands: Input messages that request something to happen (e.g., ScriptCreateCommand)
//   - Events: Output messages that indicate something has happened (e.g., ScriptCreatedEvent)
//
// # Subject Patterns
//
// All NATS subject patterns are defined as constants, with both pattern forms
// (for consumers) and builder functions (for publishers):
//
//   - Pattern constants: Used for consumer subscriptions (e.g., "command.script.*.run")
//   - Builder functions: Generate concrete subjects (e.g., ScriptRunSubject("foo") → "command.script.foo.run")
//
// # Usage Example
//
//	// Create and publish a command
//	cmd := messages.NewScriptCreateCommand("my-script", "python").
//	    WithCorrelation("req-123")
//
//	publisher := messages.NewPublisher(js)
//	if err := publisher.PublishCommand(ctx, cmd); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create and publish an event
//	evt := messages.NewScriptCreatedEvent("my-script", "python").
//	    WithCorrelation("req-123")
//
//	if err := publisher.PublishEvent(ctx, evt); err != nil {
//	    log.Fatal(err)
//	}
//
// # Domain Organization
//
// Messages are organized by domain:
//
//   - Script domain: Script creation, execution, and job lifecycle
//   - Terminal domain: Terminal commands and UI rendering events
//
// Each domain has its own set of commands and events with appropriate validation.
package messages
