# Spec: Command Forms in Binrun Layouts

## Goal

Add interactive command forms to the layout system that allow users to send typed NATS commands through the UI.

## Current State

- Layouts can contain subscription nodes that display messages
- Commands are sent via terminal or raw `/command/{name}` endpoint
- Message types in `internal/messages/` have validation but no UI representation

## Implementation

### 1. Add Command Definitions to Session

**File**: `internal/platform/types.go`

Add to `SessionInfo`:
```go
Commands map[string]CommandDef `json:"commands,omitempty"`

type CommandDef struct {
    MessageType string            `json:"message_type"` // e.g. "ScriptCreateCommand"
    Defaults    map[string]any    `json:"defaults,omitempty"`
}
```

### 2. Extend Layout Node for Commands

**File**: `internal/platform/layout.go`

Add to `LayoutNode`:
```go
Command string `json:"command,omitempty"` // references key in Commands map
```

Update `NodeType()` to return `"command"` when Command field is set.

Update `Validate()` to ensure command nodes don't have subscription/split fields.

### 3. Define Command Field Schemas

**File**: `internal/messages/schemas.go` (new)

```go
package messages

type FieldType string

const (
    FieldTypeString   FieldType = "string"
    FieldTypeSelect   FieldType = "select"
    FieldTypeArray    FieldType = "array"
    FieldTypeKeyValue FieldType = "keyvalue"
)

type FieldSchema struct {
    Name        string     `json:"name"`
    Type        FieldType  `json:"type"`
    Required    bool       `json:"required"`
    Placeholder string     `json:"placeholder,omitempty"`
    Options     []string   `json:"options,omitempty"` // for select
}

var CommandSchemas = map[string][]FieldSchema{
    "ScriptCreateCommand": {
        {Name: "script_name", Type: FieldTypeString, Required: true, Placeholder: "my-script"},
        {Name: "script_type", Type: FieldTypeSelect, Required: true, Options: []string{"python", "typescript"}},
    },
    "ScriptRunCommand": {
        {Name: "args", Type: FieldTypeArray, Required: false},
        {Name: "env", Type: FieldTypeKeyValue, Required: false},
    },
}
```

### 4. Create Command Form Component

**File**: `ui/components/command_form.templ`

```go
templ CommandForm(commandKey string, def CommandDef, schemas []FieldSchema) {
    <form class="command-form" 
          data-on-submit="@post('/command/execute', {includeLocal: true})">
        <input type="hidden" name="_messageType" value={def.MessageType}/>
        <input type="hidden" name="_commandKey" value={commandKey}/>
        
        for _, field := range schemas {
            @renderField(field, def.Defaults[field.Name])
        }
        
        <button type="submit">Execute</button>
    </form>
}

templ renderField(field FieldSchema, defaultValue any) {
    <div class="field-group">
        <label>{field.Name}</label>
        switch field.Type {
        case FieldTypeString:
            <input 
                type="text" 
                name={field.Name}
                placeholder={field.Placeholder}
                value={fmt.Sprint(defaultValue)}
                data-bind={field.Name}
                data-on-input__debounce.300ms="@post('/command/validate', {includeLocal: true})"
                data-class-error={"$" + field.Name + "Error"}
                if field.Required { required }
            />
        case FieldTypeSelect:
            <select name={field.Name} data-bind={field.Name}>
                for _, opt := range field.Options {
                    <option value={opt} 
                        if fmt.Sprint(defaultValue) == opt { selected }>
                        {opt}
                    </option>
                }
            </select>
        }
        <span class="error-msg" 
              data-show={"$" + field.Name + "Error"}
              data-text={"$" + field.Name + "ErrorMsg"}></span>
    </div>
}
```

### 5. Update Layout Tree Rendering

**File**: `ui/components/layout_tree.templ`

In `renderLayoutNode`, add case for command nodes:
```go
case "command":
    @renderCommandNode(node, panelName, path)
```

Add new function:
```go
templ renderCommandNode(node *LayoutNode, panelName string, path string) {
    // Need access to session commands here - pass through context
    <div class="layout-command" data-path={path}>
        @CommandForm(node.Command, commands[node.Command], schemas)
    </div>
}
```

### 6. Enhance SendCommand for Typed Messages

**File**: `internal/platform/handlers.go`

Replace `SendCommand` with:
```go
func SendCommand(nc *nats.Conn, js jetstream.JetStream) http.HandlerFunc {
    publisher := messages.NewPublisher(js)
    
    return func(w http.ResponseWriter, r *http.Request) {
        name := chi.URLParam(r, "name")
        
        // For typed commands from forms
        if name == "execute" {
            var data map[string]any
            json.NewDecoder(r.Body).Decode(&data)
            
            messageType := data["_messageType"].(string)
            delete(data, "_messageType")
            delete(data, "_commandKey")
            
            cmd, err := messages.BuildCommand(messageType, data)
            if err != nil {
                http.Error(w, err.Error(), 400)
                return
            }
            
            if err := publisher.PublishCommand(r.Context(), cmd); err != nil {
                http.Error(w, err.Error(), 400)
                return
            }
        } else {
            // Legacy raw publish
            var payload map[string]any
            json.NewDecoder(r.Body).Decode(&payload)
            subj := "command." + name
            data, _ := json.Marshal(payload)
            nc.Publish(subj, data)
        }
        
        w.WriteHeader(http.StatusAccepted)
    }
}
```

### 7. Add Command Builder

**File**: `internal/messages/builders.go`

Add:
```go
func BuildCommand(messageType string, data map[string]any) (Command, error) {
    switch messageType {
    case "ScriptCreateCommand":
        name, _ := data["script_name"].(string)
        scriptType, _ := data["script_type"].(string)
        return NewScriptCreateCommand(name, scriptType), nil
        
    case "ScriptRunCommand":
        name, _ := data["script_name"].(string) 
        cmd := NewScriptRunCommand(name)
        
        if args, ok := data["args"].([]any); ok {
            strArgs := make([]string, len(args))
            for i, v := range args {
                strArgs[i] = fmt.Sprint(v)
            }
            cmd.WithArgs(strArgs...)
        }
        
        if env, ok := data["env"].(map[string]any); ok {
            envMap := make(map[string]string)
            for k, v := range env {
                envMap[k] = fmt.Sprint(v)
            }
            cmd.WithEnv(envMap)
        }
        return cmd, nil
        
    default:
        return nil, fmt.Errorf("unknown message type: %s", messageType)
    }
}
```

### 8. Add Validation Endpoint

**File**: `internal/platform/handlers.go`

Add:
```go
func ValidateCommandHandler(js jetstream.JetStream) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        sse := datastar.NewSSE(w, r)
        
        var signals map[string]any
        datastar.ReadSignals(r, &signals)
        
        messageType, _ := signals["_messageType"].(string)
        if messageType == "" {
            return
        }
        
        // Build command with current field values
        cmd, err := messages.BuildCommand(messageType, signals)
        if err != nil {
            return
        }
        
        // Validate and send field-specific errors
        validationErr := cmd.Validate()
        errorSignals := make(map[string]any)
        
        for _, field := range messages.CommandSchemas[messageType] {
            errorKey := field.Name + "Error"
            errorMsgKey := field.Name + "ErrorMsg"
            
            if validationErr != nil && strings.Contains(validationErr.Error(), field.Name) {
                errorSignals[errorKey] = true
                errorSignals[errorMsgKey] = validationErr.Error()
            } else {
                errorSignals[errorKey] = false
                errorSignals[errorMsgKey] = ""
            }
        }
        
        sse.MarshalAndMergeSignals(errorSignals)
    }
}
```

Add route: `r.Post("/command/validate", ValidateCommandHandler(js))`

### 9. Pass Commands Through UI Stream

**File**: `internal/platform/ui_stream.go`

Ensure commands are available when rendering layout. The SessionInfo already contains Commands, but the layout tree component needs access. Consider passing through context or modifying the layout converter.

### 10. Add Styles

**File**: `ui/static/style.css`

```css
.command-form {
    display: flex;
    flex-direction: column;
    gap: var(--size-3);
    padding: var(--size-3);
}

.field-group {
    display: flex;
    flex-direction: column;
    gap: var(--size-1);
}

.field-group input.error,
.field-group select.error {
    border-color: var(--red-6);
}

.error-msg {
    color: var(--red-6);
    font-size: var(--font-size-0);
}
```

## Example Usage

Session KV:
```json
{
  "commands": {
    "quick-script": {
      "message_type": "ScriptCreateCommand",
      "defaults": {"script_type": "python"}
    }
  },
  "layout": {
    "panels": {
      "right": {
        "command": "quick-script"
      }
    }
  }
}
```

This renders a form in the right panel with:
- Script name text field with real-time validation
- Script type dropdown defaulting to "python"
- Execute button that sends validated command