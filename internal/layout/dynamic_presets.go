package layout

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"
)

// LoadDynamicPresets reads every key in KV bucket "layouts" and injects them
// into the global Presets map. Call once at start-up.
func LoadDynamicPresets(ctx context.Context, js jetstream.JetStream) error {
	kv, err := js.KeyValue(ctx, "layouts")
	if err != nil {
		return err
	}
	keys, _ := kv.Keys(ctx)
	for _, k := range keys {
		entry, _ := kv.Get(ctx, k)
		var p Preset
		if err := json.Unmarshal(entry.Value(), &p); err != nil {
			slog.Warn("invalid preset in KV", "key", k, "err", err)
			continue
		}
		if p.ID == "" {
			p.ID = k
		}
		Presets[p.ID] = p
	}
	slog.Info("dynamic presets loaded", "count", len(keys))
	return nil
}
