package core

import (
	"encoding/json"
	"net/http"

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
		case PresetKeyScripts:
			scriptName := r.URL.Query().Get("script")
			jobID := r.URL.Query().Get("job")
			subs = BuildScriptPreset(scriptName, jobID)
		default:
			var ok bool
			subs, ok = Presets[presetID]
			if !ok {
				http.Error(w, "unknown preset", 404)
				return
			}
		}
		sid := SessionID(r)
		kv, _ := js.KeyValue(r.Context(), "sessions")
		info := SessionInfo{Subscriptions: subs}
		data, _ := json.Marshal(info)
		if _, err := kv.Put(r.Context(), sid, data); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
