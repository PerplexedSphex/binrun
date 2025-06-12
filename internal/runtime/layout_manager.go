package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	"binrun/internal/layout"
	"binrun/internal/messages"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/nats-io/nats.go/jetstream"
)

// LayoutManager consumes command.layout.* subjects to mutate session layouts
// stored in the "sessions" KV bucket. All validation is centralised here.
type LayoutManager struct {
	js        jetstream.JetStream
	publisher *messages.Publisher
}

func NewLayoutManager(js jetstream.JetStream) *LayoutManager {
	return &LayoutManager{
		js:        js,
		publisher: messages.NewPublisher(js),
	}
}

// Start registers a durable consumer on the COMMAND stream filtered to
// command.layout.* subjects and handles messages until ctx is cancelled.
func (lm *LayoutManager) Start(ctx context.Context) error {
	// Create or update consumer on COMMAND stream
	cons, err := lm.js.CreateOrUpdateConsumer(ctx, "COMMAND", jetstream.ConsumerConfig{
		Durable:        "LAYOUT_CMD",
		FilterSubjects: []string{"command.layout.>"},
		AckPolicy:      jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return err
	}

	_, err = cons.Consume(func(m jetstream.Msg) {
		subj := m.Subject()
		switch subj {
		case "command.layout.panel.set":
			lm.applyPanelSet(ctx, m)
		case "command.layout.preset.apply":
			lm.applyPreset(ctx, m)
		case "command.layout.patch":
			lm.applyPatch(ctx, m)
		default:
			m.Term() // unknown
		}
	})
	return err
}

// ───────────────────────────── Option A ─────────────────────────────
func (lm *LayoutManager) applyPanelSet(ctx context.Context, msg jetstream.Msg) {
	var c messages.PanelSetCommand
	if json.Unmarshal(msg.Data(), &c) != nil {
		_ = msg.Term()
		return
	}

	kv, _ := lm.js.KeyValue(ctx, "sessions")
	entry, _ := kv.Get(ctx, c.SessionID)
	st, _ := layout.LoadSessionData(entry.Value())

	if st.Layout == nil {
		st.Layout = &layout.PanelLayout{Panels: map[string]*layout.LayoutNode{}}
	}
	var node layout.LayoutNode
	if json.Unmarshal(c.Node, &node) != nil {
		_ = msg.Term()
		return
	}
	st.Layout.Panels[c.Panel] = &node

	if err := st.Layout.Validate(); err != nil {
		slog.Warn("panel.set validation failed", "err", err)
		_ = msg.Nak()
		return
	}
	dataObj, _ := st.Raw()
	raw, _ := json.Marshal(dataObj)
	kv.Put(ctx, c.SessionID, raw)
	_ = msg.Ack()
}

// ───────────────────────────── Option B/F ───────────────────────────
func (lm *LayoutManager) applyPreset(ctx context.Context, msg jetstream.Msg) {
	var c messages.ApplyPresetCommand
	if json.Unmarshal(msg.Data(), &c) != nil {
		_ = msg.Term()
		return
	}

	p, ok := layout.Presets[c.PresetID]
	if !ok {
		_ = msg.Nak()
		return
	}
	built, err := p.BuildLayout(c.Args)
	if err != nil {
		_ = msg.Nak()
		return
	}

	kv, _ := lm.js.KeyValue(ctx, "sessions")
	entry, _ := kv.Get(ctx, c.SessionID)
	st, _ := layout.LoadSessionData(entry.Value())

	mode := c.Mode
	if mode == "" {
		mode = layout.PresetMergePanels
	}

	switch mode {
	case layout.PresetReplaceAll:
		st.Layout = built
	case layout.PresetMergePanels:
		if st.Layout == nil {
			st.Layout = &layout.PanelLayout{Panels: map[string]*layout.LayoutNode{}}
		}
		if built != nil {
			for pn, n := range built.Panels {
				st.Layout.Panels[pn] = n
			}
		}
	case layout.PresetSinglePanel:
		if c.Panel == "" {
			_ = msg.Nak()
			return
		}
		if st.Layout == nil {
			st.Layout = &layout.PanelLayout{Panels: map[string]*layout.LayoutNode{}}
		}
		if built != nil {
			st.Layout.Panels[c.Panel] = built.Panels[c.Panel]
		}
	default:
		// Unknown mode
		_ = msg.Nak()
		return
	}

	if st.Layout != nil {
		if err := st.Layout.Validate(); err != nil {
			_ = msg.Nak()
			return
		}
	}
	dataObj, _ := st.Raw()
	raw, _ := json.Marshal(dataObj)
	kv.Put(ctx, c.SessionID, raw)
	_ = msg.Ack()
}

// ───────────────────────────── Option C ────────────────────────────
func (lm *LayoutManager) applyPatch(ctx context.Context, msg jetstream.Msg) {
	var c messages.LayoutPatchCommand
	if json.Unmarshal(msg.Data(), &c) != nil {
		_ = msg.Term()
		return
	}

	kv, _ := lm.js.KeyValue(ctx, "sessions")
	entry, _ := kv.Get(ctx, c.SessionID)
	st, _ := layout.LoadSessionData(entry.Value())

	// Serialise current layout (nil -> {})
	current, _ := json.Marshal(st.Layout)
	if len(current) == 0 {
		current = []byte("{}")
	}

	var patched []byte
	switch c.Type {
	case messages.PatchMerge, "":
		patched, _ = jsonpatch.MergePatch(current, c.Patch)
	case messages.PatchJSONPatch:
		patch, _ := jsonpatch.DecodePatch(c.Patch)
		patched, _ = patch.Apply(current)
	default:
		_ = msg.Nak()
		return
	}

	var newLayout *layout.PanelLayout
	if len(bytes.TrimSpace(patched)) > 0 {
		if err := json.Unmarshal(patched, &newLayout); err != nil {
			_ = msg.Nak()
			return
		}
		if newLayout != nil {
			if err := newLayout.Validate(); err != nil {
				_ = msg.Nak()
				return
			}
		}
	}
	st.Layout = newLayout

	dataObj, _ := st.Raw()
	raw, _ := json.Marshal(dataObj)
	kv.Put(ctx, c.SessionID, raw)
	_ = msg.Ack()
}
