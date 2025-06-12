# Unified Session & Layout Model Design

## Goals & Assumptions

1. **Single source of truth**: We consolidate all session‐related state (subscriptions, layout, environment, and command history) into one canonical model in `internal/layout`.
2. **Backward compatibility**: UIStream, LoadPresetHandler, and TerminalEngine will continue to read/write the same KV bucket (`sessions`) without changing their public interfaces.
3. **Typed vs raw**: We separate the serialized form (stored in KV as JSON) from the in‐memory typed form used by server logic (e.g. parsed layout tree).
4. **Extensibility**: The model must support future features (e.g. tracking command history, custom panels) without breaking existing workflows.
5. **Circular‐import avoidance**: We retain the JSON converter for UI components, but the core session/layout types live entirely under `internal/layout`.

## Proposed Types

### 1. Storage Representation (raw JSON)
```go
// internal/layout/model.go
package layout

// SessionData is the JSON schema stored in the "sessions" KV bucket.
type SessionData struct {
  // Event subjects this session subscribes to
  Subscriptions  []string          `json:"subscriptions"`

  // User environment variables (terminal 'env set')
  Env            map[string]string `json:"env,omitempty"`

  // Raw JSON for user layout (PanelLayout serialized)
  Layout         json.RawMessage   `json:"layout,omitempty"`

  // History of commands entered in this session
  History        []string          `json:"history,omitempty"`
}
```

### 2. In‐Memory Typed Model
```go
// internal/layout/model.go
package layout

// SessionState is the in‐memory view of session, combining typed layout.
type SessionState struct {
  // Current subscriptions for SSE consumers
  Subscriptions  []string

  // Environment variables (for TerminalEngine)
  Env            map[string]string

  // Parsed layout tree (nil if none)
  Layout         *PanelLayout

  // Command history (last N commands)
  History        []string
}
```

### 3. Parsing & Serialization Helpers
```go
// internal/layout/model.go

// Load parses raw JSON into SessionState, including layout tree.
func LoadSessionData(raw []byte) (SessionState, error) {
  var d SessionData
  if err := json.Unmarshal(raw, &d); err != nil {
    return SessionState{}, err
  }

  var st SessionState
  st.Subscriptions = d.Subscriptions
  st.Env = d.Env
  st.History = d.History

  if len(d.Layout) > 0 {
    pl, err := ParseLayout(d.Layout)
    if err != nil {
      return st, err
    }
    st.Layout = pl
  }
  return st, nil
}

// Raw serializes SessionState back into JSON for KV storage.
func (st *SessionState) Raw() (SessionData, error) {
  var d SessionData
  d.Subscriptions = st.Subscriptions
  d.Env = st.Env
  d.History = st.History

  if st.Layout != nil {
    data, err := json.Marshal(st.Layout)
    if err != nil {
      return d, err
    }
    d.Layout = data
  }
  return d, nil
}
```

## Data Flow Adjustments

1. **UIStream**:  
   - `kv.Get` → `SessionData` → `LoadSessionData` → `SessionState`  
   - Render using `SessionState.Subscriptions` & `SessionState.Layout`  
   - On write (e.g. default entry): construct `SessionState`, call `.Raw()`, then `kv.Put`.

2. **LoadPresetHandler** & **TerminalEngine**:  
   - `kv.Get` → `LoadSessionData` → `SessionState`  
   - Mutate `Subscriptions`, `Env`, and/or `History` on the `SessionState`  
   - Call `.Raw()` → `kv.Put` (preserving Layout and other fields)

3. **Presets**:  
   - Move `runtime.Presets` into `internal/layout/presets.go`  
   - Preset builds a new `[]string` subscriptions list; assign into `SessionState.Subscriptions`  

4. **Legacy Cleanup**:  
   - Remove old `runtime.SessionInfo` type  
   - Remove duplicative `internal/platform/types.go` and `internal/platform/layout.go`  
   - Consolidate presets, model, parsing, and conversion into `internal/layout`

## Next Steps

- Review and refine the `SessionData`/`SessionState` definitions.  
- Implement the model in `internal/layout/model.go`, update all readers/writers accordingly.  
- Migrate presets into `internal/layout/presets.go` and update `LoadPresetHandler`.  
- Write tests for JSON round‐trip and layout parsing in the new package.  
- Remove all legacy session/layout code from `internal/platform` and `internal/runtime`.

---
*This design provides a clear, extensible foundation for session and layout management, unifying all related logic under `internal/layout` while preserving backward compatibility.* 