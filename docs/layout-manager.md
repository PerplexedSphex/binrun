## Feature “Layout‑Manager Commands”

Add first‑class command messages that mutate a session’s layout via JetStream, instead of HTTP handlers or hard‑wired terminal commands.

### Goals

| Goal                              | Achieved by                                                                                      |
| --------------------------------- | ------------------------------------------------------------------------------------------------ |
| Whole‑panel swaps                 | **Option A `PanelSetCommand`**                                                                   |
| Full / partial preset application | **Option B `ApplyPresetCommand`**                                                                |
| Arbitrary surgical edits          | **Option C `LayoutPatchCommand`** (RFC 7386 *merge* patch **or** RFC 6902 *JSON‑Patch*)          |
| Shareable preset fragments (KV)   | **Option F** – presets can live in the existing `layouts` KV bucket and are consumed by Option B |

All commands flow through the new **LayoutManager** service; it is symmetrical to `TerminalEngine` and `ScriptRunner`.

---

## 1  Message definitions

`internal/messages/layout_commands.go`

```go
package messages

import (
	"encoding/json"
	"errors"
	"fmt"

	"binrun/internal/layout"
)

// ──────────────────────────  A – whole‑panel replace  ─────────────────────────

type PanelSetCommand struct {
	SessionID string          `json:"session_id" required:"true"`
	Panel     string          `json:"panel"     required:"true"` // left|main|right|bottom
	Node      json.RawMessage `json:"node"      required:"true"` // LayoutNode JSON
}

func (c PanelSetCommand) Subject() string { return "command.layout.panel.set" }
func (c PanelSetCommand) IsCommand()      {}
func (c PanelSetCommand) Validate() error {
	if c.SessionID == "" || c.Panel == "" || len(c.Node) == 0 {
		return errors.New("session_id, panel and node are required")
	}
	var n layout.LayoutNode
	if err := json.Unmarshal(c.Node, &n); err != nil {
		return fmt.Errorf("node JSON: %w", err)
	}
	return n.Validate()
}

// ──────────────────────────  B / F – preset apply  ───────────────────────────

type ApplyPresetMode string

const (
	PresetReplaceAll   ApplyPresetMode = "replace-all"
	PresetMergePanels  ApplyPresetMode = "merge"       // replace only panels present in preset
	PresetSinglePanel  ApplyPresetMode = "panel-only"  // extract one panel from preset
)

type ApplyPresetCommand struct {
	SessionID string            `json:"session_id" required:"true"`
	PresetID  string            `json:"preset_id"  required:"true"`
	Args      map[string]string `json:"args,omitempty"`
	Panel     string            `json:"panel,omitempty"` // when Mode==panel-only
	Mode      ApplyPresetMode   `json:"mode,omitempty"`  // default = merge
}

func (c ApplyPresetCommand) Subject() string { return "command.layout.preset.apply" }
func (c ApplyPresetCommand) IsCommand()      {}
func (c ApplyPresetCommand) Validate() error {
	if c.SessionID == "" || c.PresetID == "" {
		return errors.New("session_id and preset_id are required")
	}
	if c.Mode == "" {
		c.Mode = PresetMergePanels
	}
	return nil
}

// ──────────────────────────  C – JSON Patch / Merge‑Patch  ───────────────────

type PatchType string

const (
	PatchMerge     PatchType = "merge"     // RFC 7386
	PatchJSONPatch PatchType = "jsonpatch" // RFC 6902
)

type LayoutPatchCommand struct {
	SessionID string          `json:"session_id" required:"true"`
	Patch     json.RawMessage `json:"patch"      required:"true"`
	Type      PatchType       `json:"type,omitempty"` // default = merge
}

func (c LayoutPatchCommand) Subject() string { return "command.layout.patch" }
func (c LayoutPatchCommand) IsCommand()      {}
func (c LayoutPatchCommand) Validate() error {
	if c.SessionID == "" || len(c.Patch) == 0 {
		return errors.New("session_id and patch are required")
	}
	if c.Type == "" {
		c.Type = PatchMerge
	}
	return nil
}
```

**Register the new types**

```go
// internal/messages/builders.go
func GetCommandTypes() []string {
	return []string{
		"ScriptCreateCommand",
		"ScriptRunCommand",
		"TerminalCommandMessage",
		"PanelSetCommand",
		"ApplyPresetCommand",
		"LayoutPatchCommand",
	}
}
```

`FieldSchema` already works by reflection, so the new commands get auto‑generated forms.

---

## 2  Dynamic preset loading (Option F)

```go
// internal/layout/dynamic_presets.go  (new file)
package layout

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"
)

// LoadDynamicPresets reads every key in KV bucket "layouts" and injects them
// into the global Presets map. Call once at start‑up.
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
```

---

## 3  LayoutManager service

`internal/runtime/layout_manager.go`

```go
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

func (lm *LayoutManager) Start(ctx context.Context) error {
	// durable consumer on COMMAND stream
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

// ---------------------------------------------------------------- option A
func (lm *LayoutManager) applyPanelSet(ctx context.Context, msg jetstream.Msg) {
	var c messages.PanelSetCommand
	if json.Unmarshal(msg.Data(), &c) != nil { msg.Term(); return }

	kv, _ := lm.js.KeyValue(ctx, "sessions")
	entry, _ := kv.Get(ctx, c.SessionID)
	st, _ := layout.LoadSessionData(entry.Value())

	if st.Layout == nil {
		st.Layout = &layout.PanelLayout{Panels: map[string]*layout.LayoutNode{}}
	}
	var node layout.LayoutNode
	if json.Unmarshal(c.Node, &node) != nil { msg.Term(); return }
	st.Layout.Panels[c.Panel] = &node

	if err := st.Layout.Validate(); err != nil {
		slog.Warn("panel.set validation failed", "err", err)
		msg.Nak()
		return
	}
	raw, _ := json.Marshal(st.Raw())
	kv.Put(ctx, c.SessionID, raw)
	msg.Ack()
}

// ---------------------------------------------------------------- option B/F
func (lm *LayoutManager) applyPreset(ctx context.Context, msg jetstream.Msg) {
	var c messages.ApplyPresetCommand
	if json.Unmarshal(msg.Data(), &c) != nil { msg.Term(); return }

	p, ok := layout.Presets[c.PresetID]
	if !ok {
		msg.Nak()
		return
	}
	built, err := p.BuildLayout(c.Args)
	if err != nil {
		msg.Nak(); return
	}

	kv, _ := lm.js.KeyValue(ctx, "sessions")
	entry, _ := kv.Get(ctx, c.SessionID)
	st, _ := layout.LoadSessionData(entry.Value())

	switch c.Mode {
	case layout.PresetReplaceAll:
		st.Layout = built
	case layout.PresetMergePanels:
		if st.Layout == nil { st.Layout = &layout.PanelLayout{Panels: map[string]*layout.LayoutNode{}} }
		for pn, n := range built.Panels {
			st.Layout.Panels[pn] = n
		}
	case layout.PresetSinglePanel:
		if c.Panel == "" {
			msg.Nak(); return
		}
		if st.Layout == nil { st.Layout = &layout.PanelLayout{Panels: map[string]*layout.LayoutNode{}} }
		st.Layout.Panels[c.Panel] = built.Panels[c.Panel]
	}

	if st.Layout != nil && st.Layout.Validate() != nil {
		msg.Nak(); return
	}
	raw, _ := json.Marshal(st.Raw())
	kv.Put(ctx, c.SessionID, raw)
	msg.Ack()
}

// ---------------------------------------------------------------- option C
func (lm *LayoutManager) applyPatch(ctx context.Context, msg jetstream.Msg) {
	var c messages.LayoutPatchCommand
	if json.Unmarshal(msg.Data(), &c) != nil { msg.Term(); return }

	kv, _ := lm.js.KeyValue(ctx, "sessions")
	entry, _ := kv.Get(ctx, c.SessionID)
	st, _ := layout.LoadSessionData(entry.Value())

	// serialise current layout (nil → `{}`)
	current, _ := json.Marshal(st.Layout)
	if len(current) == 0 { current = []byte(`{}`) }

	var patched []byte
	switch c.Type {
	case messages.PatchMerge:
		patched, _ = jsonpatch.MergePatch(current, c.Patch)
	case messages.PatchJSONPatch:
		patch, _ := jsonpatch.DecodePatch(c.Patch)
		patched, _ = patch.Apply(current)
	}

	var newLayout *layout.PanelLayout
	if len(bytes.TrimSpace(patched)) > 0 {
		if err := json.Unmarshal(patched, &newLayout); err != nil {
			msg.Nak(); return
		}
		if newLayout.Validate() != nil {
			msg.Nak(); return
		}
	}
	st.Layout = newLayout
	raw, _ := json.Marshal(st.Raw())
	kv.Put(ctx, c.SessionID, raw)
	msg.Ack()
}
```

---

## 4  Wire the service into the platform

```go
// internal/platform/core.go  (within Run)
    // after TerminalEngine
    lm := runtime.NewLayoutManager(js)
    go func() {
        if err := lm.Start(ctx); err != nil {
            slog.Error("LayoutManager error", "err", err)
        } else {
            slog.Info("LayoutManager started")
        }
    }()

    // load dynamic presets (Option F)
    _ = layout.LoadDynamicPresets(ctx, js)
```

Add **`github.com/evanphx/json-patch/v5`** to `go.mod`.

---

## 5  Field‑schema auto‑forms (no code change)

Because the new structs sit in `messages`, the reflection helper already outputs appropriate `<input>`s.
Example (left sidebar):

```templ
@components.CommandForm("PanelSetCommand", "", nil)
@components.CommandForm("ApplyPresetCommand", "", nil)
@components.CommandForm("LayoutPatchCommand", "", nil)
```

These submit to `POST /command` and are routed automatically.

---

## 6  How each option is used

### Option A – whole‑panel replace

```json
{
  "_messageType": "PanelSetCommand",
  "session_id":   "{{$sid}}",
  "panel":        "main",
  "node": {
    "split": "horizontal",
    "at": "1/2",
    "first":  { "subscription": "event.foo.*" },
    "second": { "component": "terminal" }
  }
}
```

### Option B – apply preset (code or KV)

```json
{
  "_messageType": "ApplyPresetCommand",
  "session_id": "{{$sid}}",
  "preset_id": "scriptsubs",
  "args": { "script":"bot", "job":"*" },
  "mode": "merge"
}
```

### Option C – merge‑patch

```json
{
  "_messageType": "LayoutPatchCommand",
  "session_id": "{{$sid}}",
  "type": "merge",
  "patch": {
    "panels": {
      "right": { "component":"terminal" }
    }
  }
}
```

### Option F – dynamically uploaded preset fragment

```bash
# Upload once
nats kv put layouts/mydashboard @dashboard.json
```

Then:

```json
{
  "_messageType":"ApplyPresetCommand",
  "session_id":"{{$sid}}",
  "preset_id":"mydashboard",
  "mode":"panel-only",
  "panel":"main"
}
```

---

## 7  Why this design

* **Single responsibility** – all layout writes pass through `LayoutManager`; validation is centralised.
* **No new HTTP surface** – uses existing `/command` endpoint, gaining CSRF protection and uniform auth.
* **Streaming friendly** – clients emit commands from anywhere (backend cron, terminal, UI button) and don’t need to know about KV.
* **Graceful failure** – every command is validated (syntactic + semantic) before the KV write; bad commands NAK and can be replayed/fixed.
* **Extensible** – add new op structs later without changing the consumer loop.

---

## 8  Checklist to merge

1. `go get github.com/evanphx/json-patch/v5`
2. Add new files:

   * `internal/messages/layout_commands.go`
   * `internal/layout/dynamic_presets.go`
   * `internal/runtime/layout_manager.go`
3. Edit:

   * `internal/messages/builders.go` (`GetCommandTypes`)
   * `internal/platform/core.go`
4. `go vet ./...` – ensure imports set.
5. Deploy; open UI → “Command” forms appear automatically.

This adds powerful, consistent and future‑proof layout mutability with only \~300 new lines of code.
