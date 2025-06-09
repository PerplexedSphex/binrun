package layout

import (
	"encoding/json"
)

// CommandDescriptor holds info to render a CommandForm in the UI.
type CommandDescriptor struct {
	// MessageType e.g. "ScriptRunCommand"
	Type string `json:"type"`
	// Script name for script-specific commands, if applicable
	Script string `json:"script,omitempty"`
	// Default field values for the form
	Defaults map[string]any `json:"defaults,omitempty"`
}

// SessionData is the JSON schema stored in the "sessions" KV bucket.
type SessionData struct {
	// Event subjects this session subscribes to
	Subscriptions []string `json:"subscriptions"`

	// User environment variables (terminal 'env set')
	Env map[string]string `json:"env,omitempty"`

	// Raw JSON for user layout (PanelLayout serialized)
	Layout json.RawMessage `json:"layout,omitempty"`

	// History of commands entered in this session
	History []string `json:"history,omitempty"`

	// Commands to display as forms in the UI
	Commands []CommandDescriptor `json:"commands,omitempty"`
}

// SessionState is the in-memory view of session, combining typed layout.
type SessionState struct {
	// Current subscriptions for SSE consumers
	Subscriptions []string

	// Environment variables (for TerminalEngine)
	Env map[string]string

	// Parsed layout tree (nil if none)
	Layout *PanelLayout

	// Command history (last N commands)
	History []string

	// Commands to render as forms
	Commands []CommandDescriptor
}

// LoadSessionData parses raw JSON into SessionState, including the layout tree.
func LoadSessionData(raw []byte) (SessionState, error) {
	var d SessionData
	if err := json.Unmarshal(raw, &d); err != nil {
		return SessionState{}, err
	}

	st := SessionState{
		Subscriptions: d.Subscriptions,
		Env:           d.Env,
		History:       d.History,
		Commands:      d.Commands,
	}

	if len(d.Layout) > 0 {
		pl, err := ParseLayout(d.Layout)
		if err != nil {
			return st, err
		}
		st.Layout = pl
	}

	return st, nil
}

// Raw converts SessionState back into a SessionData for JSON storage.
func (st *SessionState) Raw() (SessionData, error) {
	d := SessionData{
		Subscriptions: st.Subscriptions,
		Env:           st.Env,
		History:       st.History,
		Commands:      st.Commands,
	}

	if st.Layout != nil {
		data, err := json.Marshal(st.Layout)
		if err != nil {
			return d, err
		}
		d.Layout = data
	}

	return d, nil
}
