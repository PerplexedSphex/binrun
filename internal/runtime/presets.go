package runtime

import "fmt"

// Preset keys (visible to users / API)
const (
	PresetKeyOrderSubs  = "ordersubs"
	PresetKeyChatSubs   = "chatsubs"
	PresetKeyScriptSubs = "scriptsubs"
)

// Subject constants that are reused across presets
const (
	SubjectOrders = "event.orders.*"
	SubjectChat   = "event.chat.*"
)

// Preset describes a group of subject filters, optionally parameterised.
// Params lists accepted flag names (e.g. "script", "job").
// Build returns the subject list for a given arg map.
// Missing params should be treated as wildcard "*" by Build.
// Only Build OR Static may be set – Build(nil) should behave like Static.

type Preset struct {
	Help   string
	Params []string
	Build  func(args map[string]string) []string
}

// allPresets is the registry keyed by preset ID.
var Presets = map[string]Preset{}

func init() {
	// orders → static subjects
	Presets[PresetKeyOrderSubs] = Preset{
		Help:   "Order and invoice events",
		Params: nil,
		Build: func(_ map[string]string) []string {
			return []string{SubjectOrders, "event.invoice.created"}
		},
	}

	// chat → static subjects
	Presets[PresetKeyChatSubs] = Preset{
		Help: "Chat events",
		Build: func(_ map[string]string) []string {
			return []string{SubjectChat, "event.script.>"}
		},
	}

	// scriptsubs → parameterised by script / job
	Presets[PresetKeyScriptSubs] = Preset{
		Help:   "Script lifecycle events",
		Params: []string{"script", "job"},
		Build:  buildScriptPreset,
	}
}

// buildScriptPreset implements Build for the scripts preset.
func buildScriptPreset(args map[string]string) []string {
	scriptName := args["script"]
	jobID := args["job"]
	if scriptName == "" {
		scriptName = "*"
	}
	if jobID == "" {
		jobID = "*"
	}
	base := fmt.Sprintf("event.script.%s.job.%s.", scriptName, jobID)
	return []string{base + "started", base + "exit", base + "stdout", base + "stderr"}
}
