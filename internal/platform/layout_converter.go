package platform

import (
	components "binrun/ui/components"
	"encoding/json"
)

// ConvertLayoutForUI converts a platform.PanelLayout to the components version
// to avoid import cycles. This uses JSON marshaling/unmarshaling as a simple
// way to copy the data between identical structures.
func ConvertLayoutForUI(layout *PanelLayout) (*components.PanelLayout, error) {
	if layout == nil {
		return nil, nil
	}

	// Marshal to JSON
	data, err := json.Marshal(layout)
	if err != nil {
		return nil, err
	}

	// Unmarshal to components version
	var uiLayout components.PanelLayout
	if err := json.Unmarshal(data, &uiLayout); err != nil {
		return nil, err
	}

	return &uiLayout, nil
}
