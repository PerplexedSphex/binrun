package platform

import "encoding/json"

// SessionInfo is stored in the KV bucket keyed by session ID.
type SessionInfo struct {
	Subscriptions []string        `json:"subscriptions"`
	Layout        json.RawMessage `json:"layout,omitempty"`
}
