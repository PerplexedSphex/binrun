package layout

import "binrun/internal/messages"

// Re-export preset mode constants so that runtime package can use layout.Preset* names
const (
	PresetReplaceAll  = messages.PresetReplaceAll
	PresetMergePanels = messages.PresetMergePanels
	PresetSinglePanel = messages.PresetSinglePanel
)
