package platform

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"binrun/internal/messages"
	"binrun/internal/runtime"

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

		preset, ok := runtime.Presets[presetID]
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

		subs := preset.Build(args)
		// Ensure terminal subscription is included
		sid := SessionID(r)
		termSubj := messages.TerminalFreezeSubject(sid)
		if !slices.Contains(subs, termSubj) {
			subs = append(subs, termSubj)
		}
		slices.Sort(subs) // Sort for consistency

		// Update the session KV
		kv, err := js.KeyValue(r.Context(), "sessions")
		if err != nil {
			slog.Error("LoadPresetHandler: Failed to get KV", "err", err)
			http.Error(w, "internal error", 500)
			return
		}

		// Get existing session info to preserve env vars
		var info runtime.SessionInfo
		if entry, err := kv.Get(r.Context(), sid); err == nil && entry != nil {
			_ = json.Unmarshal(entry.Value(), &info)
		}

		// Update subscriptions while preserving env
		info.Subscriptions = subs

		data, _ := json.Marshal(info)
		if _, err := kv.Put(r.Context(), sid, data); err != nil {
			slog.Error("LoadPresetHandler: Failed to put KV", "sid", sid, "err", err)
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// TerminalCommandHandler publishes the JSON body to terminal.session.<sid>.command.
func TerminalCommandHandler(js jetstream.JetStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := SessionID(r)
		publisher := messages.NewPublisher(js)

		var cmdText string
		// Attempt to parse as form (handles urlencoded and multipart).
		if err := r.ParseMultipartForm(10 << 20); err == nil && (len(r.Form) > 0 || len(r.PostForm) > 0 || (r.MultipartForm != nil && len(r.MultipartForm.Value) > 0)) {
			cmdText = r.FormValue("cmd")
		} else {
			// fallback to JSON body
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad body", 400)
				return
			}
			if cmd, ok := body["cmd"].(string); ok {
				cmdText = cmd
			}
		}

		if cmdText == "" {
			http.Error(w, "missing cmd", 400)
			return
		}

		cmd := messages.NewTerminalCommandMessage(sid, cmdText)
		if err := publisher.PublishCommand(r.Context(), cmd); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
