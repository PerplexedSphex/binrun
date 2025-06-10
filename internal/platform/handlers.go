package platform

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"binrun/internal/layout"
	"binrun/internal/messages"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Health returns 200 OK.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// SendCommand handles all typed command submissions
func SendCommand(nc *nats.Conn, js jetstream.JetStream) http.HandlerFunc {
	publisher := messages.NewPublisher(js)

	return func(w http.ResponseWriter, r *http.Request) {
		var data map[string]any
		var err error

		// Parse request body - support multipart/form-data, x-www-form-urlencoded, and JSON
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(contentType, "application/json") {
			// Parse JSON
			if err = json.NewDecoder(r.Body).Decode(&data); err != nil {
				http.Error(w, "invalid JSON", 400)
				return
			}
		} else if strings.Contains(contentType, "multipart/form-data") {
			// Parse multipart form data
			// The constant 10 << 20 limits the total memory used for parts to 10MB.
			if err = r.ParseMultipartForm(10 << 20); err != nil {
				http.Error(w, "invalid multipart form data", 400)
				return
			}
			data = make(map[string]any)
			for key, values := range r.Form {
				if len(values) == 1 {
					data[key] = values[0]
				} else {
					data[key] = values
				}
			}
		} else {
			// Default to parsing standard form data (handles x-www-form-urlencoded and query params)
			if err = r.ParseForm(); err != nil {
				http.Error(w, "invalid form data", 400)
				return
			}
			data = make(map[string]any)
			for key, values := range r.Form {
				if len(values) == 1 {
					data[key] = values[0]
				} else {
					data[key] = values
				}
			}
		}

		// Extract message type
		messageType, ok := data["_messageType"].(string)
		if !ok {
			http.Error(w, "missing _messageType", 400)
			return
		}

		// Remove meta fields
		delete(data, "_messageType")

		// Add session ID from middleware for commands that need it
		if messageType == "TerminalCommandMessage" {
			sessionID := SessionID(r)
			if sessionID == "" {
				http.Error(w, "missing session ID", 400)
				return
			}
			data["session_id"] = sessionID
		}

		// Build typed command
		cmd, err := messages.BuildCommand(messageType, data)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// Validate before sending
		if err := cmd.Validate(); err != nil {
			http.Error(w, fmt.Sprintf("validation error: %v", err), 400)
			return
		}

		// Publish using typed publisher
		if err := publisher.PublishCommand(r.Context(), cmd); err != nil {
			http.Error(w, fmt.Sprintf("publish error: %v", err), 500)
			return
		}

		// Return standardized response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "sent",
			"type":   messageType,
		})
	}
}

// Handler to load a preset's subscriptions into the session
func LoadPresetHandler(js jetstream.JetStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presetID := chi.URLParam(r, "preset")

		preset, ok := layout.Presets[presetID]
		if !ok {
			http.Error(w, "unknown preset", http.StatusNotFound)
			return
		}

		// Build argument map from query parameters (script, job, etc.)
		args := map[string]string{}
		for key, vals := range r.URL.Query() {
			if len(vals) > 0 {
				args[key] = vals[0]
			}
		}

		// Build and set layout for the preset
		sid := SessionID(r)

		// Update the session KV
		kv, err := js.KeyValue(r.Context(), "sessions")
		if err != nil {
			slog.Error("LoadPresetHandler: Failed to get KV", "err", err)
			http.Error(w, "internal error", 500)
			return
		}

		// Load existing session state to preserve env and layout
		entry, err := kv.Get(r.Context(), sid)
		state := layout.SessionState{}
		if err == nil && entry != nil {
			st, err2 := layout.LoadSessionData(entry.Value())
			if err2 == nil {
				state = st
			}
		}
		// Update layout only
		builtLayout, err2 := preset.BuildLayout(args)
		if err2 != nil {
			slog.Error("LoadPresetHandler: failed to build layout", "preset", presetID, "err", err2)
			http.Error(w, err2.Error(), http.StatusInternalServerError)
			return
		}
		state.Layout = builtLayout
		// Serialize and put
		dataObj, _ := state.Raw()
		raw, _ := json.Marshal(dataObj)
		if _, err := kv.Put(r.Context(), sid, raw); err != nil {
			slog.Error("LoadPresetHandler: Failed to put KV", "sid", sid, "err", err)
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
