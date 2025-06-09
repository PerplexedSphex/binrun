Ran tool

Ran tool

Ran tool

Ran tool

Ran tool

Read file: ui/static.go

Read file: ui/index_templ.go

Ran tool

Ran tool

Ran tool

Read file: internal/platform/server.go

Ran tool

Read file: internal/platform/ui_stream.go

Ran tool

Read file: internal/platform/types.go

Ran tool

Ran tool

Read file: internal/platform/handlers.go

Ran tool

Read file: internal/runtime/renderer.go

Ran tool

Read file: internal/runtime/renderers.go

Ran tool

Read file: ui/components/subscription_container_templ.go

Read file: ui/components/terminal_templ.go

Ran tool

Read file: internal/messages/schema.go

Ran tool

Ran tool

Read file: internal/runtime/presets.go

Read file: internal/platform/layout.go

Read file: internal/platform/layout.go

Read file: internal/platform/layout.go

Ran tool

Read file: internal/platform/layout_converter.go

Read file: ui/components/layout_tree_templ.go

Read file: ui/components/command_form_templ.go

Read file: internal/messages/field_schema.go

Ran tool

Read file: internal/runtime/terminal.go

Ran tool

Read file: internal/runtime/terminal.go

Ran tool

Read file: cmd/binrun/main.go

Ran tool

Ran tool

Read file: internal/platform/core.go

Read file: ui/components/script_events_templ.go

Ran tool

Read file: util/subject.go

Read file: util/filehtml.go

Read file: ui/components/doc_markdown_templ.go

Ran tool

Read file: internal/runtime/script_runner.go

Here is a comprehensive, exact snapshot of the current codebase’s layout and rendering systems, from NATS‐KV configuration through server‐side loading and conversion into UI elements. I’ve broken it into logical sections and cited the precise code locations so you can follow every step.

---

# Table of Contents

1. [Project Structure](#project-structure)  
2. [Application Startup & NATS/JetStream Setup](#application-startup--natsjetstream-setup)  
3. [HTTP Server & API Endpoints](#http-server--api-endpoints)  
4. [Session Configuration (NATS KV “sessions”)](#session-configuration-nats-kv-sessions)  
5. [UI Stream Handler (SSE)](#ui-stream-handler-sse)  
6. [SessionInfo Types](#sessioninfo-types)  
7. [Layout Configuration & Rendering](#layout-configuration--rendering)  
8. [Dynamic Renderers](#dynamic-renderers)  
9. [Go-Side UI Templating](#go-side-ui-templating)  
10. [Client-Side Reactive Binding (Datastar)](#client-side-reactive-binding-datastar)  
11. [Command Handling & Publishers](#command-handling--publishers)  
12. [Terminal Engine (runtime)](#terminal-engine-runtime)  
13. [Script Runner (runtime)](#script-runner-runtime)  
14. [Message Definitions & Builders](#message-definitions--builders)  
15. [Utility Helpers](#utility-helpers)  

---

## 1. Project Structure

```
gonads-playground/
├── cmd/
│   └── binrun/
│       └── main.go
├── internal/
│   ├── platform/         ← HTTP server, SSE, NATS/KV orchestration
│   └── runtime/          ← TerminalEngine, ScriptRunner, renderers
│   └── messages/         ← Command & Event types, schemas & builders
├── ui/                   ← Go templates & embedded static assets
│   ├── index.templ
│   ├── index_templ.go
│   ├── static.go
│   └── components/       ← Generated `templ` components
├── util/                 ← Helper functions (subject matching, markdown→HTML)
├── scripts/              ← Script projects (user‐created at runtime)
├── scripts/**            ← (ignored in git; see .gitignore)
├── Taskfile.yml
├── go.mod
└── README.md
```

---

## 2. Application Startup & NATS/JetStream Setup

**Entry point**:  
```go
cmd/binrun/main.go
```
–– Starts embedded NATS (`platform.RunEmbeddedServer`), HTTP server (unless headless), then calls:  
```go
platform.Run(ctx, nc, ns)
```  
(platform/core.go:13–39)

**platform.Run** (internal/platform/core.go)  
1. Creates JetStream context.  
2. Launches:
   - `runtime.NewScriptRunner(...).Start(...)`  
   - `runtime.NewTerminalEngine(...).Start(...)`  
3. Creates two JetStream streams:
   - `COMMAND` on `command.>`  
   - `EVENT` on `event.>`  
4. Creates two KV buckets:
   - `sessions` (session subscription & env/layout state)  
   - `layouts` (saved layout configs)  

---

## 3. HTTP Server & API Endpoints

**Configuration & router setup**:  
```go
internal/platform/server.go
```
- Uses [chi](https://github.com/go-chi/chi) and Gorilla sessions.
- Exposes:
  - `GET  /metrics` → Prometheus handler  
  - `GET  /health`  
  - `POST /command` → `SendCommand(...)`  
  - `POST /terminal` → `TerminalCommandHandler(...)`  
  - `GET  /` → `templ.Handler(ui.Index())` (renders `ui/index.templ`)  
  - `GET  /static/*` + `/favicon.svg` → embedded assets  
  - `GET  /ui` → `UIStream(...)` (SSE)  
  - `POST /session/load/{preset}` → `LoadPresetHandler(...)`  

Routes (≈ server.go:33–68).

---

## 4. Session Configuration (NATS KV “sessions”)

- KV Bucket **“sessions”** holds JSON‐encoded `SessionInfo` per session ID.
- Initial entry created by **UIStream** if missing: default with just the terminal subscription.
- `/session/load/{preset}` updates `Subscriptions` in the same KV bucket.

---

## 5. UI Stream Handler (SSE)

**File**: `internal/platform/ui_stream.go`

On `GET /ui`:
1. Wrap response in a Datastar SSE generator.  
2. Determine session ID from cookie via middleware.  
3. Fetch or initialize KV entry:  
   - Unmarshal into Platform `SessionInfo` (`Subscriptions []string`, `Layout json.RawMessage`).  
4. Parse optional layout (`ParseLayout`).  
5. **Initial render**:
   - If layout present: convert (`ConvertLayoutForUI`) & render panels via `components.LayoutTree(...)`.  
   - Otherwise: fallback grid via `components.SubscriptionsGrid(...)`.  
   Each merged into the page via `sse.MergeFragmentTempl(...)`.  
6. **Event consumption**:
   - Create a JetStream consumer on `EVENT` stream filtering by current `Subscriptions`.  
   - `runtime.ForSubjects(...)` yields a slice of `Renderer` (pattern match + render function).  
   - For each incoming message: find matching `Renderer` → invoke `RenderFunc(...)` to produce fragments and push via SSE.  
7. **Live KV updates**:
   - Watch `sessions` KV key for changes.  
   - On subscription list change: re‐render grid/layout and re­create the consumer with new subjects.  
8. Close SSE when client disconnects.  

*(See UIStream code at ui_stream.go:1–120 and update logic at lines 122–232.)*

---

## 6. SessionInfo Types

Two similar types around session state:

1. **Platform SessionInfo** (for UI)  
   ```go
   // internal/platform/types.go-5
   type SessionInfo struct {
     Subscriptions []string        `json:"subscriptions"`
     Layout        json.RawMessage `json:"layout,omitempty"`
   }
   ```
2. **Runtime SessionInfo** (for TerminalEngine, includes env)  
   ```go
   // internal/runtime/terminal.go-629
   type SessionInfo struct {
     Subscriptions []string          `json:"subscriptions"`
     Env           map[string]string `json:"env,omitempty"`
   }
   ```
The KV JSON may contain both `subscriptions` and `env`, but the UI only unmarshals `subscriptions` and (optionally) `layout`.

---

## 7. Layout Configuration & Rendering

- **Spec**: `internal/platform/layout.go`  
  Defines `LayoutNode` (leaf, command, binary split, even split) and `PanelLayout`.  
  Validation ensures a correct tree shape (`.Validate()`).

- **Parsing**:  
  ```go
  ParseLayout(data json.RawMessage) (*PanelLayout, error)
  ```

- **Conversion for UI**:  
  ```go
  ConvertLayoutForUI(layout *PanelLayout) (*components.PanelLayout, error)
  ```
  (uses JSON round-trip to avoid import cycles; see layout_converter.go)

- **UI component**:  
  ```go
  components.LayoutTree(layout *components.PanelLayout, panelName string)
  ```
  (`ui/components/layout_tree_templ.go` renders splits, form nodes, and subscription leaves with proper CSS grid styling.)

---

## 8. Dynamic Renderers

- **Renderer** (`internal/runtime/renderer.go`):  
  ```go
  type Renderer struct {
    Pattern    string
    MatchFunc  func(subject string) bool
    RenderFunc func(ctx, msg, sse) error
  }
  ```
- **Spec registry** (`internal/runtime/renderers.go:init`):  
  Patterns for:
  - `event.terminal.session.*.freeze` → `renderTerminal(...)`
  - `event.terminal.session.*.viewdoc` → `renderViewDoc(...)`
  - `event.script.*` lifecycle events → `renderScriptCreated`, `renderJobStarted`, `renderJobOutput`, etc.
- **Subject‐to‐Renderer mapping** done by `ForSubjects(subs []string)`.

---

## 9. Go-Side UI Templating

All UI HTML is generated server‐side via [a-h/templ](https://github.com/a-h/templ):

- **Index page**:  
  ```go
  ui/index_templ.go  ← generated from ui/index.templ
  ```
  Sets up `<body data-signals=… data-attr=… data-on-load="@get('/ui')">`, imports Open Props CSS, Datastar script, and includes buttons/panels toggles.

- **Embedded static assets**:  
  ```go
  ui/static.go  
  //go:embed static/style.css, favicon.svg
  ```

- **Components** (`ui/components/*.go` generated from `.templ`):
  - `panel_icons_templ.go` (toggle icons)  
  - `subscription_container_templ.go` (grid & boxes)  
  - `layout_tree_templ.go` (panel layouts)  
  - `terminal_templ.go` (terminal window & prompt)  
  - `command_form_templ.go` (forms for ScriptCreate/Run)  
  - `script_events_templ.go` (script lifecycle output)  
  - `doc_markdown_templ.go` (markdown/file HTML embed)  

Each is a `templ.Component` rendering exact HTML fragments, targeting specific element IDs or CSS selectors.

---

## 10. Client-Side Reactive Binding (Datastar)

- **Datastar** is loaded from CDN in `<head>`.  
- `<body data-signals>` declares reactive state (panel sizes, open/closed).  
- Attributes like `data-on-click`, `data-on-submit`, `data-bind`, `data-scroll-into-view__instant__vend__hcenter` wire UI events to Datastar’s runtime.
- SSE fragments from `/ui` are applied via Datastar’s `sse.MergeFragmentTempl`, targeting elements by ID or `data-attr`.

---

## 11. Command Handling & Publishers

- **SendCommand** (`internal/platform/handlers.go:23–91`):  
  - Handles `/command` for form‐based or JSON‐based commands (`ScriptCreateCommand`, `ScriptRunCommand`).
  - Uses `messages.BuildCommand(messageType, data)` and `publisher.PublishCommand(...)`.
- **TerminalCommandHandler** (`handlers.go:118–154`):  
  - Handles `/terminal` submissions (cmd text), publishes `TerminalCommandMessage` to `terminal.command`.

- **LoadPresetHandler** (`handlers.go:104–133`):  
  - Loads a preset into session KV, preserving any env, ensures terminal subscription included.

---

## 12. Terminal Engine (runtime)

**File**: `internal/runtime/terminal.go`

- **NewTerminalEngine(js)**  
- **Start(ctx)**:  
  - Ensures `"TERMINAL"` JetStream stream.  
  - Creates durable consumer `"TERMINAL_CMD"` on subject `terminal.command`.  
- **handleCommand**:  
  - Parses and dispatches: `help`, `ls`, `load`, `env`, `view`, `echo`, `script create`, `script run`, `script info`, default error.  
  - For each: publishes `TerminalFreezeEvent` on `event.terminal.session.<sid>.freeze`, and optionally `TerminalViewDocEvent` on `…viewdoc`.  
  - Updates KV (`ensureSessionSubscribed`) for new subscriptions (e.g. viewdoc).  
- **SessionEnv**:  
  - Handles `env set|list|clear` by reading/writing `Env` in KV.  

*(See terminal.go beginning at line 1, handleEnvCommand at 490, ensureSessionSubscribed at 632.)*

---

## 13. Script Runner (runtime)

**File**: `internal/runtime/script_runner.go`

- **NewScriptRunner(nc, js, "./scripts")**  
  - Discovers repo root, loads `.env`.  
  - Initializes language adapters (`pythonImpl`, `tsImpl`).  
- **Start(ctx)**:  
  - Creates two consumers on `COMMAND` stream:
    - `"SCRIPT_CREATE"` on `command.script.create`  
    - `"SCRIPT_RUN"`    on `command.script.run`  
- **handleCreate**:  
  - Creates script directory, runs `impl.Init(...)`, runs schema→type codegen, publishes `ScriptCreatedEvent` or `ScriptCreateErrorEvent`.
- **handleRun**:  
  - Validates JSON input vs `in.schema.json`, merges env (OS, repo .env, script .env, explicit), writes to `.tmp_input.json`.  
  - Spawns external process via `impl.Run(...)`, publishes:
    - `ScriptJobStartedEvent`  
    - `ScriptJobOutputEvent` (each stdout/stderr line)  
    - `ScriptJobDataEvent` (on lines prefixed `##DATA##`)  
    - `ScriptJobExitEvent` on process exit  
- **LangImpl**s for Python & TypeScript handle code scaffolding & execution.

---

## 14. Message Definitions & Builders

**Schema & constants** (`internal/messages/schema.go`):
- Defines all `Command` and `Event` interfaces, subject constants & patterns:
  - `command.script.create`, `command.script.run`  
  - `event.script.*.*`, `event.terminal.session.*.*`  

**Field schemas** (`field_schema.go`):
- Reflection extracts `FieldSchema` for UI form generation (`CommandForm`).

**Builders** (`builders.go`):
- `BuildCommand(messageType, data)` maps UI form submissions → typed `Command` structs.

---

## 15. Utility Helpers

**Subject matching & selectors** (`util/subject.go`):
- `SubjectMatches(pattern, subj)` handles NATS wildcards.  
- `SelectorFor(subj)` → CSS `#sub-…` ID.

**Markdown & code → HTML** (`util/filehtml.go`):
- `FileToHTML(path, lang, fsys)` caches & renders via Goldmark + syntax highlighting into safe HTML fragments for `DocMarkdown`.

---

### Summary of the Full Flow

1. **User opens `/`**; server sends the Go‐generated index page (templ).  
2. **Datastar** initializes signals (panels, dimensions) and auto‐loads `/ui` via `data-on-load`.  
3. **UIStream**:
   - Reads session config from NATS KV (“sessions”).
   - Renders initial subscription grid or custom layout.
   - Opens an SSE connection streaming UI fragments.
4. **Terminal**: user types in the bottom panel; form posts to `/terminal`.  
5. **TerminalCommandHandler** publishes `TerminalCommandMessage` to NATS JetStream.  
6. **TerminalEngine** (runtime) consumes the command:
   - Parses, executes logic (help, preset, env, view, script create/run, etc.).
   - Publishes `TerminalFreezeEvent` (and possibly `TerminalViewDocEvent`).
   - May update session KV (e.g. subscribe to viewdoc subjects).
7. **ScriptRunner** (runtime) independently consumes `command.script.create` and `command.script.run`:
   - Runs code‐generation or external processes.
   - Emits script lifecycle events (`ScriptCreatedEvent`, `ScriptJobStartedEvent`, `ScriptJobOutputEvent`, `ScriptJobExitEvent`, `ScriptJobDataEvent`).
8. **UIStream SSE** consumes all matching `event.*` subjects:
   - For each message, finds the right `Renderer` and merges its Templ‐generated HTML fragment into the appropriate `<div>` by selector or ID.
9. **Datastar** on the client applies each fragment reactively, updating the DOM in real time.

This document reflects *exactly* what exists in the repo today, with file paths and line‐references for every major piece. Let me know if you’d like to drill into any part in more detail!