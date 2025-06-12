package layout

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed presets/*.json
var presetsFS embed.FS

// Preset defines a named bundle of subscriptions, commands, and layout
// loaded from presets/*.json
// Params lists parameters (e.g. "script", "job") for placeholder substitution.
// Subscriptions patterns may include placeholders `{param}` to be replaced at build time.
type Preset struct {
	ID            string              `json:"id"`
	Help          string              `json:"help"`
	Params        []string            `json:"params,omitempty"`
	Subscriptions []string            `json:"subscriptions"`
	Commands      []CommandDescriptor `json:"commands,omitempty"`
	Layout        *PanelLayout        `json:"layout,omitempty"`
}

// Presets is the in-memory registry of all loaded presets
var Presets map[string]Preset

func init() {
	Presets = map[string]Preset{}
	entries, err := presetsFS.ReadDir("presets")
	if err != nil {
		panic(fmt.Errorf("reading presets directory: %w", err))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, err := presetsFS.ReadFile("presets/" + entry.Name())
		if err != nil {
			panic(fmt.Errorf("reading preset %s: %w", entry.Name(), err))
		}
		var p Preset
		if err := json.Unmarshal(raw, &p); err != nil {
			panic(fmt.Errorf("parsing preset %s: %w", entry.Name(), err))
		}
		if p.ID == "" {
			p.ID = strings.TrimSuffix(entry.Name(), ".json")
		}
		Presets[p.ID] = p
	}
}

// Build applies args to the subscription patterns, substituting placeholders
// for each Param (defaulting to "*" if missing).
func (p Preset) Build(args map[string]string) []string {
	out := make([]string, 0, len(p.Subscriptions))
	for _, pattern := range p.Subscriptions {
		s := pattern
		for _, param := range p.Params {
			val := args[param]
			if val == "" {
				val = "*"
			}
			s = strings.ReplaceAll(s, "{"+param+"}", val)
		}
		out = append(out, s)
	}
	return out
}

// BuildCommands applies args to the command descriptors, producing concrete forms.
func (p Preset) BuildCommands(args map[string]string) []CommandDescriptor {
	out := make([]CommandDescriptor, 0, len(p.Commands))
	for _, cmd := range p.Commands {
		c := cmd // copy
		for _, param := range p.Params {
			val := args[param]
			if val == "" {
				val = "*"
			}
			c.Script = strings.ReplaceAll(c.Script, "{"+param+"}", val)
			// Substitute in defaults if string
			for k, v := range c.Defaults {
				if s, ok := v.(string); ok {
					c.Defaults[k] = strings.ReplaceAll(s, "{"+param+"}", val)
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// BuildLayout applies args to the preset's Layout structure in a type-safe manner
// and returns a concrete PanelLayout or nil if none defined.
func (p Preset) BuildLayout(args map[string]string) (*PanelLayout, error) {
	if p.Layout == nil {
		return nil, nil
	}
	// Helper to substitute placeholders in strings
	substitute := func(s string) string {
		for _, param := range p.Params {
			val := args[param]
			if val == "" {
				val = "*"
			}
			s = strings.ReplaceAll(s, "{"+param+"}", val)
		}
		return s
	}
	// Copy and substitute defaults map
	copyDefaults := func(orig map[string]any) map[string]any {
		if orig == nil {
			return nil
		}
		dst := make(map[string]any, len(orig))
		for k, v := range orig {
			if str, ok := v.(string); ok {
				dst[k] = substitute(str)
			} else {
				dst[k] = v
			}
		}
		return dst
	}
	// Recursive builder for LayoutNode
	var buildNode func(src *LayoutNode) *LayoutNode
	buildNode = func(src *LayoutNode) *LayoutNode {
		if src == nil {
			return nil
		}
		dst := &LayoutNode{
			Subscription: substitute(src.Subscription),
			Component:    substitute(src.Component),
			Command:      substitute(src.Command),
			Script:       substitute(src.Script),
			Defaults:     copyDefaults(src.Defaults),
			Split:        src.Split,
			At:           src.At,
			Direction:    src.Direction,
		}
		dst.First = buildNode(src.First)
		dst.Second = buildNode(src.Second)
		if src.Items != nil {
			dst.Items = make([]*LayoutNode, len(src.Items))
			for i, item := range src.Items {
				dst.Items[i] = buildNode(item)
			}
		}
		return dst
	}
	// Build final PanelLayout
	out := &PanelLayout{Panels: make(map[string]*LayoutNode, len(p.Layout.Panels))}
	for name, node := range p.Layout.Panels {
		out.Panels[name] = buildNode(node)
	}
	return out, nil
}
