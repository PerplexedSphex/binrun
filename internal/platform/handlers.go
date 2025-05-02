package platform

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

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

// SendCommand publishes to command.{name} subject.
func SendCommand(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if name == "" {
			http.Error(w, "missing command name", http.StatusBadRequest)
			return
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		subj := "command." + name
		data, _ := json.Marshal(payload)
		if err := nc.Publish(subj, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"published": subj})
	}
}

// Handler to load a preset's subscriptions into the session
func LoadPresetHandler(js jetstream.JetStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presetID := chi.URLParam(r, "preset")
		var subs []string
		switch presetID {
		case runtime.PresetKeyScripts:
			scriptName := r.URL.Query().Get("script")
			jobID := r.URL.Query().Get("job")
			subs = runtime.BuildScriptPreset(scriptName, jobID)
		default:
			var ok bool
			subs, ok = runtime.Presets[presetID]
			if !ok {
				http.Error(w, "unknown preset", 404)
				return
			}
		}
		// Ensure terminal subscription is included
		sid := SessionID(r)
		termSubj := fmt.Sprintf("terminal.session.%s.event", sid)
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
		var body map[string]any

		// Attempt to parse as form (handles urlencoded and multipart).
		if err := r.ParseMultipartForm(10 << 20); err == nil && (len(r.Form) > 0 || len(r.PostForm) > 0 || (r.MultipartForm != nil && len(r.MultipartForm.Value) > 0)) {
			lineID := r.FormValue("line_id")
			cmd := r.FormValue("cmd")
			body = map[string]any{"line_id": lineID, "cmd": cmd}
		} else {
			// fallback to JSON body
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad body", 400)
				return
			}
		}
		subj := fmt.Sprintf("terminal.session.%s.command", sid)
		data, _ := json.Marshal(body)
		if _, err := js.Publish(r.Context(), subj, data); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
