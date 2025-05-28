package messages

import (
	"reflect"
	"strings"
)

// FieldType represents the UI input type for a field
type FieldType string

const (
	FieldTypeString   FieldType = "string"
	FieldTypeSelect   FieldType = "select"
	FieldTypeBoolean  FieldType = "boolean"
	FieldTypeArray    FieldType = "array"
	FieldTypeKeyValue FieldType = "keyvalue"
)

// FieldSchema describes how a field should be rendered in the UI
type FieldSchema struct {
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required"`
	Placeholder string    `json:"placeholder,omitempty"`
	Options     []string  `json:"options,omitempty"` // for select
	JSONName    string    `json:"json_name"`         // actual field name in JSON
}

// GetFieldSchemas extracts field schemas from a message type using reflection
func GetFieldSchemas(messageType string) []FieldSchema {
	var msg interface{}
	switch messageType {
	case "ScriptCreateCommand":
		msg = &ScriptCreateCommand{}
	case "ScriptRunCommand":
		msg = &ScriptRunCommand{}
	default:
		return nil
	}

	return extractFieldSchemas(msg)
}

// extractFieldSchemas uses reflection to build field schemas from struct tags
func extractFieldSchemas(v interface{}) []FieldSchema {
	var schemas []FieldSchema

	t := reflect.TypeOf(v).Elem()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip fields without json tag or with omitempty only fields
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" || jsonTag == "correlation_id,omitempty" {
			continue
		}

		// Skip fields marked as derived from subject
		if strings.Contains(field.Tag.Get("json"), "-") && !strings.Contains(field.Tag.Get("json"), ",") {
			continue
		}

		// Parse json tag to get field name
		jsonName := strings.Split(jsonTag, ",")[0]

		// Build schema from tags
		schema := FieldSchema{
			Name:     field.Name,
			JSONName: jsonName,
			Type:     FieldTypeString, // default
		}

		// Check required
		if required := field.Tag.Get("required"); required == "true" {
			schema.Required = true
		}

		// Get placeholder
		if placeholder := field.Tag.Get("placeholder"); placeholder != "" {
			schema.Placeholder = placeholder
		}

		// Determine field type
		if fieldType := field.Tag.Get("field_type"); fieldType != "" {
			switch fieldType {
			case "select":
				schema.Type = FieldTypeSelect
				// Parse options
				if options := field.Tag.Get("options"); options != "" {
					schema.Options = strings.Split(options, ",")
				}
			case "boolean":
				schema.Type = FieldTypeBoolean
			}
		} else {
			// Infer from Go type
			switch field.Type.Kind() {
			case reflect.Bool:
				schema.Type = FieldTypeBoolean
			case reflect.Slice:
				schema.Type = FieldTypeArray
			case reflect.Map:
				schema.Type = FieldTypeKeyValue
			default:
				schema.Type = FieldTypeString
			}
		}

		schemas = append(schemas, schema)
	}

	return schemas
}

// GetCommandMessageTypes returns all available command message types
func GetCommandMessageTypes() []string {
	return []string{
		"ScriptCreateCommand",
		"ScriptRunCommand",
	}
}
