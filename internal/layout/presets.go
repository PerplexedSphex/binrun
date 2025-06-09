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

// BuildLayout applies args to the preset's Layout structure, substituting placeholders
// and returns a concrete PanelLayout or nil if none defined.
func (p Preset) BuildLayout(args map[string]string) (*PanelLayout, error) {
	if p.Layout == nil {
		return nil, nil
	}
	// Marshal the template layout to JSON
	data, err := json.Marshal(p.Layout)
	if err != nil {
		return nil, fmt.Errorf("marshal preset layout: %w", err)
	}
	s := string(data)
	// Substitute each param
	for _, param := range p.Params {
		val := args[param]
		if val == "" {
			val = "*"
		}
		s = strings.ReplaceAll(s, "{"+param+"}", val)
	}
	// Unmarshal into PanelLayout
	var out PanelLayout
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("unmarshal built layout: %w", err)
	}
	return &out, nil
}
