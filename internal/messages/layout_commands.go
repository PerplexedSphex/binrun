package messages

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ──────────────────────────  A – whole-panel replace  ─────────────────────────

// PanelSetCommand replaces the complete LayoutNode assigned to a single panel
// (left|main|right|bottom) for the given session.
//
// The Node payload must be a valid layout.LayoutNode JSON object.
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
	// Validate panel name
	switch c.Panel {
	case "left", "main", "right", "bottom":
	default:
		return fmt.Errorf("invalid panel: %s", c.Panel)
	}
	// Validate the node JSON by deserialising into LayoutNode then calling its Validate.
	var test map[string]any
	if err := json.Unmarshal(c.Node, &test); err != nil {
		return fmt.Errorf("node JSON: %w", err)
	}
	return nil
}

// ──────────────────────────  B / F – preset apply  ───────────────────────────

type ApplyPresetMode string

const (
	// Replace the entire session layout with the preset produced layout
	PresetReplaceAll ApplyPresetMode = "replace-all"
	// Merge: replace only panels that are present in the preset
	PresetMergePanels ApplyPresetMode = "merge"
	// Extract a single panel from the preset and apply only that
	PresetSinglePanel ApplyPresetMode = "panel-only"
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

// ──────────────────────────  C – JSON Patch / Merge-Patch  ───────────────────

type PatchType string

const (
	PatchMerge     PatchType = "merge"     // RFC 7386
	PatchJSONPatch PatchType = "jsonpatch" // RFC 6902
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
