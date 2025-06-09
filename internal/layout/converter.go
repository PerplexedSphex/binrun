package layout

import (
	components "binrun/ui/components"
)

// ConvertToComponents maps a PanelLayout to the ui/components.PanelLayout type.
func ConvertToComponents(src *PanelLayout) *components.PanelLayout {
	if src == nil || src.Panels == nil {
		return nil
	}
	dest := &components.PanelLayout{Panels: make(map[string]*components.LayoutNode)}
	for name, node := range src.Panels {
		dest.Panels[name] = convertNode(node)
	}
	return dest
}

// convertNode recursively copies a LayoutNode to its ui/components counterpart.
func convertNode(src *LayoutNode) *components.LayoutNode {
	if src == nil {
		return nil
	}
	dest := &components.LayoutNode{
		Subscription: src.Subscription,
		Command:      src.Command,
		Script:       src.Script,
		Defaults:     src.Defaults,
		Split:        src.Split,
		At:           src.At,
		Direction:    src.Direction,
	}
	if src.First != nil {
		dest.First = convertNode(src.First)
	}
	if src.Second != nil {
		dest.Second = convertNode(src.Second)
	}
	if src.Items != nil {
		dest.Items = make([]*components.LayoutNode, len(src.Items))
		for i, item := range src.Items {
			dest.Items[i] = convertNode(item)
		}
	}
	return dest
}
