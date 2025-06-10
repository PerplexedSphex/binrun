package runtime

import (
	"binrun/internal/layout"
	"context"
)

// CommandResult defines what the terminal should display after execution.
type CommandResult struct {
	Output string
}

// TerminalCommand is the interface for all terminal commands.
type TerminalCommand interface {
	// Name returns the command name, e.g. "help".
	Name() string
	// Help returns the help text for the command.
	Help() string
	// Execute runs the command against the given session, state, and arguments,
	// returning a new SessionState and a CommandResult for output.
	Execute(ctx context.Context, sessionID string, state layout.SessionState, args []string) (layout.SessionState, CommandResult)
}
