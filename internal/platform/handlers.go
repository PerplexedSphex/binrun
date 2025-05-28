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

// SendCommand accepts a command name in the URL, and some JSON payload.
// It publishes the payload to the subject "command.<name>".
func SendCommand(nc *nats.Conn, js jetstream.JetStream) http.HandlerFunc {
	publisher := messages.NewPublisher(js)

	return func(w http.ResponseWriter, r *http.Request) {
		// Extract the command path after /command/
		path := strings.TrimPrefix(r.URL.Path, "/command/")

		// Handle typed commands from forms
		if path == "execute" {
			var data map[string]any

			// Check if this is form data
			contentType := r.Header.Get("Content-Type")
			if strings.Contains(contentType, "multipart/form-data") || strings.Contains(contentType, "application/x-www-form-urlencoded") {
				// Parse form data
				if err := r.ParseForm(); err != nil {
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
			} else {
				// Parse JSON
				if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
					http.Error(w, "invalid JSON", 400)
					return
				}
			}

			messageType, ok := data["_messageType"].(string)
			if !ok {
				http.Error(w, "missing _messageType", 400)
				return
			}

			// Remove meta fields
			delete(data, "_messageType")

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

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status": "sent",
				"type":   messageType,
			})
		} else if strings.HasPrefix(path, "script/") && strings.HasSuffix(path, "/run") {
			// Handle script-specific run commands
			// Extract script name from URL: script/{scriptName}/run
			parts := strings.Split(path, "/")
			if len(parts) != 3 || parts[0] != "script" || parts[2] != "run" {
				http.Error(w, "invalid script run URL", 400)
				return
			}
			scriptName := parts[1]

			// Parse form data
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid form data", 400)
				return
			}

			// Build command data
			data := make(map[string]any)
			for key, values := range r.Form {
				if len(values) == 1 {
					data[key] = values[0]
				} else {
					data[key] = values
				}
			}

			// Create ScriptRunCommand
			cmd := messages.NewScriptRunCommand(scriptName)

			// Handle args if provided
			if args, ok := data["args"].(string); ok && args != "" {
				cmd = cmd.WithArgs(strings.Fields(args)...)
			}

			// Handle env if provided
			if envStr, ok := data["env"].(string); ok && envStr != "" {
				// Parse key=value pairs (one per line)
				envMap := make(map[string]string)
				lines := strings.Split(envStr, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						envMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
					}
				}
				cmd = cmd.WithEnv(envMap)
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

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status": "sent",
				"script": scriptName,
			})
		} else {
			// Legacy raw command handling
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "invalid JSON", 400)
				return
			}

			subj := "command." + path
			data, _ := json.Marshal(payload)

			if err := nc.Publish(subj, data); err != nil {
				http.Error(w, "failed to publish", 500)
				return
			}

			w.WriteHeader(http.StatusAccepted)
		}
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
		info := SessionInfo{Subscriptions: subs}
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
