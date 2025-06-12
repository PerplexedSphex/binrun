package layout

import (
	"binrun/internal/messages"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// LayoutNode represents a node in the layout tree.
// It can be a leaf (subscription), binary split, or even split.
type LayoutNode struct {
	// For leaf nodes
	Subscription string `json:"subscription,omitempty"`

	// For document nodes: a list of file paths to render as markdown
	DocumentPaths []string `json:"document_paths,omitempty"`

	// For built-in component nodes (e.g. "terminal")
	Component string `json:"component,omitempty"`

	// For command nodes
	Command  string         `json:"command,omitempty"`  // message type e.g. "ScriptCreateCommand"
	Script   string         `json:"script,omitempty"`   // script name for script-specific commands
	Defaults map[string]any `json:"defaults,omitempty"` // default field values

	// For binary splits
	Split  string      `json:"split,omitempty"` // "horizontal" | "vertical"
	At     string      `json:"at,omitempty"`    // "1/2", "1/3", "2/3", "1/4", "3/4"
	First  *LayoutNode `json:"first,omitempty"`
	Second *LayoutNode `json:"second,omitempty"`

	// For even splits
	Direction string        `json:"direction,omitempty"` // when split is "even-N"
	Items     []*LayoutNode `json:"items,omitempty"`
}

// PanelLayout represents the complete layout configuration
type PanelLayout struct {
	Panels map[string]*LayoutNode `json:"panels"`
}

// NodeType returns the type of this layout node
func (n *LayoutNode) NodeType() string {
	if len(n.DocumentPaths) > 0 {
		return "document"
	}
	if n.Component != "" {
		return "component"
	}
	if n.Subscription != "" {
		return "leaf"
	}
	if n.Command != "" {
		return "command"
	}
	if n.Split == "horizontal" || n.Split == "vertical" {
		return "binary"
	}
	if strings.HasPrefix(n.Split, "even-") {
		return "even"
	}
	return "unknown"
}

// Validate checks if the layout node is valid according to the spec
func (n *LayoutNode) Validate() error {
	switch n.NodeType() {
	case "document":
		return n.validateDocument()
	case "component":
		return n.validateComponent()
	case "leaf":
		return n.validateLeaf()
	case "command":
		return n.validateCommand()
	case "binary":
		return n.validateBinary()
	case "even":
		return n.validateEven()
	default:
		return fmt.Errorf("invalid node type '%s'", n.NodeType())
	}
}

func (n *LayoutNode) validateLeaf() error {
	// Leaf must have only subscription
	if n.Command != "" || n.Defaults != nil || n.Split != "" || n.At != "" || n.First != nil || n.Second != nil ||
		n.Direction != "" || n.Items != nil {
		return fmt.Errorf("leaf node must only have subscription field")
	}
	if n.Subscription == "" {
		return fmt.Errorf("leaf node must have subscription")
	}
	return nil
}

func (n *LayoutNode) validateCommand() error {
	// Command must have only command and optionally defaults
	if n.Subscription != "" || n.Split != "" || n.At != "" || n.First != nil || n.Second != nil ||
		n.Direction != "" || n.Items != nil {
		return fmt.Errorf("command node must only have command, script, and defaults fields")
	}
	if n.Command == "" {
		return fmt.Errorf("command node must have command")
	}
	// Validate script field is present for script-specific commands
	if n.Command == "ScriptRunCommand" && n.Script == "" {
		return fmt.Errorf("ScriptRunCommand requires script field")
	}
	return nil
}

func (n *LayoutNode) validateBinary() error {
	// Binary must have split, at, first, second
	if n.Subscription != "" || n.Direction != "" || n.Items != nil {
		return fmt.Errorf("binary split must only have split, at, first, second fields")
	}
	if n.Split != "horizontal" && n.Split != "vertical" {
		return fmt.Errorf("binary split must be 'horizontal' or 'vertical'")
	}
	if !isValidFraction(n.At) {
		return fmt.Errorf("invalid fraction '%s'", n.At)
	}
	if n.First == nil || n.Second == nil {
		return fmt.Errorf("binary split must have both first and second nodes")
	}
	// Validate children
	if err := n.First.Validate(); err != nil {
		return fmt.Errorf("first node: %w", err)
	}
	if err := n.Second.Validate(); err != nil {
		return fmt.Errorf("second node: %w", err)
	}
	return nil
}

func (n *LayoutNode) validateEven() error {
	// Even must have split, direction, items
	if n.Subscription != "" || n.At != "" || n.First != nil || n.Second != nil {
		return fmt.Errorf("even split must only have split, direction, items fields")
	}

	// Extract N from "even-N"
	matches := regexp.MustCompile(`^even-(\d)$`).FindStringSubmatch(n.Split)
	if len(matches) != 2 {
		return fmt.Errorf("invalid even split format '%s'", n.Split)
	}

	expectedCount, _ := strconv.Atoi(matches[1])
	if expectedCount < 2 || expectedCount > 5 {
		return fmt.Errorf("even split must be even-2 through even-5")
	}

	if n.Direction != "horizontal" && n.Direction != "vertical" {
		return fmt.Errorf("even split direction must be 'horizontal' or 'vertical'")
	}

	if len(n.Items) != expectedCount {
		return fmt.Errorf("even-%d must have exactly %d items, got %d",
			expectedCount, expectedCount, len(n.Items))
	}

	// Validate children
	for i, item := range n.Items {
		if item == nil {
			return fmt.Errorf("item %d is nil", i)
		}
		if err := item.Validate(); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
	}
	return nil
}

// validateComponent ensures only the Component field is set on a component node
func (n *LayoutNode) validateComponent() error {
	if n.Component == "" {
		return fmt.Errorf("component node must have component field")
	}
	// All other fields must be empty or nil
	if n.Subscription != "" || n.Command != "" || n.Script != "" || n.Defaults != nil ||
		n.Split != "" || n.At != "" || n.First != nil || n.Second != nil ||
		n.Direction != "" || n.Items != nil {
		return fmt.Errorf("component node must only have component field")
	}
	return nil
}

// validateDocument ensures only the DocumentPaths field is set on a document node
func (n *LayoutNode) validateDocument() error {
	if len(n.DocumentPaths) == 0 {
		return fmt.Errorf("document node must have at least one path")
	}
	// All other fields must be empty or nil
	if n.Subscription != "" || n.Component != "" || n.Command != "" || n.Script != "" || n.Defaults != nil ||
		n.Split != "" || n.At != "" || n.First != nil || n.Second != nil ||
		n.Direction != "" || n.Items != nil {
		return fmt.Errorf("document node must only have document_paths field")
	}
	return nil
}

// Validate checks if the panel layout is valid
func (p *PanelLayout) Validate() error {
	if p.Panels == nil {
		return fmt.Errorf("panels must not be nil")
	}

	// Check only allowed panel names
	for name := range p.Panels {
		if name != "left" && name != "main" && name != "right" && name != "bottom" {
			return fmt.Errorf("invalid panel name '%s'", name)
		}
	}

	// Validate each panel's layout
	for name, node := range p.Panels {
		if node == nil {
			continue // Empty panel is OK
		}
		if err := node.Validate(); err != nil {
			return fmt.Errorf("panel %s: %w", name, err)
		}
	}

	return nil
}

// ParseLayout parses JSON into a validated PanelLayout
func ParseLayout(data json.RawMessage) (*PanelLayout, error) {
	if len(data) == 0 {
		return nil, nil // No layout is valid
	}

	var layout PanelLayout
	if err := json.Unmarshal(data, &layout); err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}

	if err := layout.Validate(); err != nil {
		return nil, fmt.Errorf("validate layout: %w", err)
	}

	return &layout, nil
}

// isValidFraction checks if a string is a valid fraction from the spec
func isValidFraction(s string) bool {
	validFractions := []string{"1/2", "1/3", "2/3", "1/4", "3/4"}
	for _, f := range validFractions {
		if s == f {
			return true
		}
	}
	return false
}

// GetSubscriptions returns all subscription leaf nodes in the layout
func (n *LayoutNode) GetSubscriptions() []string {
	if n == nil {
		return nil
	}

	switch n.NodeType() {
	case "leaf":
		return []string{n.Subscription}
	case "binary":
		subs := n.First.GetSubscriptions()
		subs = append(subs, n.Second.GetSubscriptions()...)
		return subs
	case "even":
		var subs []string
		for _, item := range n.Items {
			subs = append(subs, item.GetSubscriptions()...)
		}
		return subs
	default:
		return nil
	}
}

// GetSubscriptions returns all subscriptions referenced in the layout
func (p *PanelLayout) GetSubscriptions() []string {
	if p == nil || p.Panels == nil {
		return nil
	}

	var subs []string
	for _, node := range p.Panels {
		subs = append(subs, node.GetSubscriptions()...)
	}
	return subs
}

// LayoutFragments is no longer provided in layout package. UI rendering is handled in platform.

// getRequiredSubscriptions returns all NATS subjects this node requires, including built-in components.
func (n *LayoutNode) getRequiredSubscriptions(sessionID string) []string {
	if n == nil {
		return nil
	}
	// Built-in component subscriptions
	if n.Component != "" {
		switch n.Component {
		case "terminal":
			return []string{messages.TerminalFreezeSubject(sessionID)}
		}
		return nil
	}
	// Leaf subscription
	if n.Subscription != "" {
		return []string{n.Subscription}
	}
	// Binary split
	if n.Split == "horizontal" || n.Split == "vertical" {
		subs := n.First.getRequiredSubscriptions(sessionID)
		subs = append(subs, n.Second.getRequiredSubscriptions(sessionID)...)
		return subs
	}
	// Even split
	if strings.HasPrefix(n.Split, "even-") {
		var subs []string
		for _, item := range n.Items {
			subs = append(subs, item.getRequiredSubscriptions(sessionID)...)
		}
		return subs
	}
	return nil
}

// GetRequiredSubscriptions traverses the entire layout and returns a deduplicated list of required NATS subjects.
func (p *PanelLayout) GetRequiredSubscriptions(sessionID string) []string {
	if p == nil || p.Panels == nil {
		return nil
	}
	var subs []string
	for _, node := range p.Panels {
		subs = append(subs, node.getRequiredSubscriptions(sessionID)...)
	}
	// Deduplicate
	sort.Strings(subs)
	unique := make([]string, 0, len(subs))
	for i, s := range subs {
		if i == 0 || s != subs[i-1] {
			unique = append(unique, s)
		}
	}
	return unique
}
