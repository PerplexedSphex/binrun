package core

import "fmt"

const (
	PresetKeyOrders  = "orders"
	PresetKeyChat    = "chat"
	SubjectOrders    = "event.orders.*"
	SubjectChat      = "event.chat.*"
	PresetKeyScripts = "scripts"
)

// Static presets (non-parameterised)
var Presets = map[string][]string{
	PresetKeyOrders: {SubjectOrders, "event.invoice.created"},
	PresetKeyChat:   {SubjectChat, "event.script.>"},
}

// BuildScriptPreset returns subject filters for the script preset.
// Empty parameters are treated as wildcards ("*").
func BuildScriptPreset(scriptName, jobID string) []string {
	if scriptName == "" {
		scriptName = "*"
	}
	if jobID == "" {
		jobID = "*"
	}
	base := fmt.Sprintf("event.script.%s.job.%s.", scriptName, jobID)
	return []string{base + "started", base + "exit", base + "stdout", base + "stderr"}
}
