Here is the standalone refactoring document.

---

# Architecture Decision Record: Unifying the UI State Model

## Context

The current `binrun` application employs a powerful but convoluted architecture for its real-time user interface. The UI state is managed through a combination of a `SessionState` object in a NATS K/V store, a separate `PanelLayout` definition, and complex logic within the `UIStream` SSE handler. This has resulted in several architectural issues:

1.  **A "Crisis of Identity"**: The true source of truth for the UI is ambiguous. Logic is spread across state models, HTTP handlers, and real-time renderers, making the system difficult to reason about and maintain.
2.  **Dueling Command Pipelines**: The system has two separate, inconsistent handlers for processing user commands (`/command` and `/terminal`).
3.  **Tangled Responsibilities in `UIStream`**: The SSE handler has become a "god object," managing session creation, layout rendering, real-time event consumer lifecycle, and state change detection.
4.  **Redundant Data Structures**: The UI layout definition is duplicated between the `internal/layout` package and the `ui/components` package, requiring a boilerplate `converter` function to bridge them.

The desired architecture is an "immediate mode" rendering philosophy, where the backend has absolute authority over the UI. The UI should be a pure, reactive function of a single, declarative state object.

## Decision

We will refactor the system to adopt a strict, unidirectional data flow centered around a single, authoritative state object: the `PanelLayout`. This refactoring will clarify responsibilities, eliminate redundancy, and make the system more robust and maintainable.

The guiding principle will be: **The `PanelLayout` struct stored in the `sessions` K/V bucket *is* the UI. Everything else is a mechanism to manipulate or render this state.**

The new data flow will be:
`HTTP Command` → `State Manipulator` → `Update K/V State` → `UIStream Watcher` → `Render State & Reconcile Consumer` → `Push to Client`

## The Refactoring Plan

### Phase 1: Unify and Consolidate State and Command Handling

This phase establishes a single source of truth for UI state and a single pipeline for modifying it.

#### **Action 1.1: Unify the Command Pipeline**
*   **Problem**: Two endpoints (`/command`, `/terminal`) handle commands with different logic.
*   **Solution**: Consolidate all command processing into the `/command` endpoint. The terminal will become a standard client of this endpoint.
*   **Tasks**:
    1.  Modify `ui/components/terminal.templ`: Change the form's `data-on-submit` to point to `/command` and add a hidden input `<input type="hidden" name="_messageType" value="TerminalCommandMessage">`.
    2.  Delete `platform.TerminalCommandHandler` and its associated route in `platform/server.go`. The existing logic in `platform.SendCommand` is sufficient.

#### **Action 1.2: Make `PanelLayout` the Authoritative UI State**
*   **Problem**: The `SessionState` struct is a "junk drawer" mixing UI layout, NATS subscription config, and runtime `Env` variables.
*   **Solution**: Elevate `PanelLayout` to be the sole descriptor of the UI's structure and content requirements. Subscriptions will be derived directly from the layout.
*   **Tasks**:
    1.  In `internal/layout/model.go`, modify `SessionData` and `SessionState` to remove the `Subscriptions`, `Commands`, and `History` fields. The new `SessionState` will primarily contain the `Layout *PanelLayout` and `Env map[string]string`.
    2.  In `internal/layout/layout.go`, create a new method: `(p *PanelLayout) GetRequiredSubscriptions() []string`. This method will traverse the layout tree and collect all `Subscription` strings from leaf nodes, returning a deduplicated list. This makes the layout fully self-describing.

### Phase 2: Decouple UI Rendering from Backend Logic

This phase disentangles the backend's responsibilities, making `UIStream` a simple orchestrator and empowering the `templ` components.

#### **Action 2.1: Eliminate Redundant Layout Structs**
*   **Problem**: `LayoutNode` and `PanelLayout` are defined in both `internal/layout` and `ui/components`, requiring a pointless `converter.go`.
*   **Solution**: Use the canonical `internal/layout` structs directly in the UI components.
*   **Tasks**:
    1.  Delete the local struct definitions in `ui/components/layout_tree.templ.go`.
    2.  Add `import "binrun/internal/layout"` to `ui/components/layout_tree.templ` and update function signatures to use the imported types.
    3.  Delete the file `internal/layout/converter.go`.

#### **Action 2.2: Refactor `UIStream` into a Two-Phase Orchestrator**
*   **Problem**: `UIStream` is a complex "god object" managing layout, state, and NATS consumer lifecycles.
*   **Solution**: Simplify `UIStream` to be a pure orchestrator that reacts to state changes by triggering two distinct phases: a layout update and a consumer reconciliation.
*   **New `UIStream` Logic**:
    1.  **On Connection**: Establish the SSE stream, load the initial `PanelLayout`, render it, start a `SessionConsumer` (see Action 2.3), and begin watching the K/V entry.
    2.  **On K/V Change**:
        *   **Phase 1 (Layout Update)**: Load the new `PanelLayout`, re-render all panels using `components.LayoutTree`, and send the new HTML fragments to the client.
        *   **Phase 2 (Consumer Reconciliation)**: Stop the old NATS consumer, then start a new one configured with the subscriptions derived from the *new* `PanelLayout`.

#### **Action 2.3: Encapsulate NATS Consumer Logic**
*   **Problem**: The logic for creating, running, and stopping the JetStream consumer is complex and tangled within `UIStream`.
*   **Solution**: Create a dedicated `SessionConsumer` helper struct to manage the consumer lifecycle.
*   **Tasks**:
    1.  Create a new file `platform/consumer_manager.go` (or similar).
    2.  Define a `SessionConsumer` struct containing the `jetstream.JetStream` context, `datastar.SSE` generator, and its own `context.Context` and `cancel` function.
    3.  Implement `Start(subjects []string)` and `Stop()` methods. `Start` will configure and run the consumer in a goroutine. `Stop` will gracefully cancel the context and wait for the goroutine to exit.
    4.  The `Start` method will be responsible for setting the `DeliverPolicy` (e.g., `DeliverAllPolicy` or `DeliverLastPerSubjectPolicy`) based on application needs.
    5.  `UIStream` will now simply call `consumerManager.Stop()` and `consumerManager.Start(newSubs)` during the reconciliation phase.

### Phase 3: Align State Manipulators with the New Architecture

This phase updates the `presets` system to work as pure manipulators of the `PanelLayout` state.

#### **Action 3.1: Implement Type-Safe Preset Building**
*   **Problem**: `presets.BuildLayout` uses fragile and error-prone `strings.ReplaceAll` on a marshaled JSON string.
*   **Solution**: Replace this with a type-safe, recursive build function.
*   **Tasks**:
    1.  Delete the current implementation of `presets.BuildLayout`.
    2.  Create a new `(p *Preset) BuildLayout(args map[string]string) *PanelLayout` method.
    3.  This method will programmatically traverse the preset's `LayoutNode` tree in memory, safely substituting placeholder values in fields like `Subscription` and `Script`.
    4.  The method will return a fully resolved, valid `PanelLayout` object.

## Expected Outcome

This refactoring will result in a significantly cleaner, more robust, and more maintainable system.
*   **Clarity**: The data flow will be simple and unidirectional. The role of each component will be unambiguous.
*   **Decoupling**: The backend logic will be decoupled from the frontend's DOM structure.
*   **Maintainability**: Adding or changing UI elements will primarily involve modifying the declarative `PanelLayout` data structure and ensuring a corresponding `templ` component exists, rather than tracing logic through multiple handlers and files.
*   **True Immediate Mode Architecture**: The system will fully embody the philosophy of a backend-authoritative UI, where state changes are atomically reflected in the user's view.