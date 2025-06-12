# Loose Ends Investigation & Design Options

This document examines four areas of the codebase where current session management, layout, command renderers, and terminal integration show inconsistencies or incomplete designs. For each area, we:

- Identify existing contradictions or gaps.
- Outline what needs to be rectified.
- Propose opinionated options for a more elegant, powerful design.

---

## 1. Session Management

### Current State & Contradictions

- There are two session info types:
  - `internal/runtime.SessionInfo` (with `Subscriptions` + `Env`) used by TerminalEngine.
  - `internal/platform/types.SessionInfo` (with `Subscriptions` + `Layout`) used by UIStream.
- Presets (`runtime.Presets`) and `LoadPresetHandler` mutate `runtime.SessionInfo` in KV.
- UIStream only reads `platform.SessionInfo`; it ignores `Env`.
- TerminalEngine writes session state (subscriptions + env) back to the same KV bucket.
- No clear owner of the KV schema: platform vs runtime both write to the same bucket.

### Rectifications Needed

- Unify a single `SessionInfo` definition (Subscriptions + Env + Layout).
- Clarify KV schema: choose one canonical struct and share between platform & runtime.
- Clean up legacy code: remove duplicate preset definitions and consolidate preset logic.

### Opinionated Options

- **Option A: One True Session Model**  
  Define a shared `internal/session` package with single `SessionInfo{Subscriptions,Env,Layout}`.  
  Both UIStream and TerminalEngine import it; remove both legacy types.

- **Option B: Separate KV Buckets per Concern**  
  Keep separate KV buckets: `sessions-sub` (subscriptions), `sessions-env` (env), `layouts` (layout).  
  Platform reads only what it needs; reduces coupling between runtime & UI.

- **Option C: Session Service API**  
  Replace raw KV with a small service layer exposing typed REST (or gRPC) endpoints for querying and mutating session fields.

---

## 2. Session Type & Layout Renderer

### Current State & Gaps

- Layout is stored as raw JSON in `SessionInfo.Layout` but no HTTP endpoint exists to update layout.
- Layout parsing & conversion occur only in UIStream; no UI controls exist to save or modify panel layouts beyond default grid.
- No decoupled service or API for persisting or loading layouts beyond KV watching.

### Rectifications Needed

- Add an endpoint to set/update the `Layout` field in session KV (e.g. `POST /session/layout`).
- Enhance UI with layout builder (drag/drop or JSON editor) that sends new layout JSON to server.
- Ensure layout validity via `ParseLayout().Validate()` before saving.

### Opinionated Options

- **Option A: Layout API & Builder**  
  Implement `POST /session/layout` to accept `PanelLayout` JSON and save it.  
  UI exposes a "Customize Layout" mode with live preview.

- **Option B: Declarative Layout Presets**  
  Extend `runtime.Presets` to include layout presets; treat both subscription and layout presets uniformly.

- **Option C: Client-Only Layout Storage**  
  Store layout in browser (localStorage), and sync to KV only on explicit "Save," reducing server load for transient adjustments.

---

## 3. Command Renderers & Layout Integration

### Current State & Issues

- `CommandForm` components are only shown when layout nodes include command types. In default grid, only subscriptions display.
- No built-in command panel; every command form must be manually added to layout JSON.
- Command output (script events) appears in subscription boxes, not integrated alongside forms.

### Rectifications Needed

- Introduce a default commands panel, rendering all available command forms by default.
- Unify command renderers into the generic subscription/grid system so commands are treated like subscriptions (subject patterns).

### Opinionated Options

- **Option A: Command Preset Panel**  
  Add a built-in "Commands" panel (left sidebar) that automatically renders `CommandForm` for each command type.

- **Option B: Dynamic Command Subscriptions**  
  Expose each command type as a subject; subscribe and render a corresponding form via `RendererSpec`.

- **Option C: Dedicated UI Module**  
  Extract command rendering into its own component chunk, draggable into any layout panel.

---

## 4. Terminal Integration Consistency

### Current State & Anomalies

- Terminal is hardcoded into the right panel in `index_templ.go` rather than dynamically subscribed via layout.
- TerminalEngine handles commands and freeze/viewdoc events specially, outside the generic subscription mechanism.
- No route to treat the terminal as a normal subscription in layout JSON.

### Rectifications Needed

- Remove hardcoded `Terminal()` call in the template; treat `event.terminal.session.*.freeze` as a normal subject in default presets.
- Allow users to move or disable the terminal panel via layout configuration.

### Opinionated Options

- **Option A: Terminal as Subscription**  
  Register `Terminal` via `RendererSpec` under wildcard pattern, include it in default subscription presets.  
  Remove fixed placement in `index.templ`.

- **Option B: Terminal Mode Toggle**  
  Offer "Terminal Mode" vs "Layout Mode" toggle, sharing the same underlying SSE pipeline.

- **Option C: Integrated REPL Component**  
  Build a standalone REPL component that can be embedded anywhere, fully managed by Datastar signals and fragment rendering.

---

*Choose the option that best balances incremental refactoring with your long-term UX goals.* 