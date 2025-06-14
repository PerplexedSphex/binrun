# binrun – Embedded JetStream Playground

## Program Abstract

binrun is a self-contained Go application that demonstrates advanced NATS JetStream patterns using an **in-process, ephemeral NATS server**. It supports two modes:

- **Business Logic Mode:** Runs a dynamic, auto-reconfiguring event system. Streams, key-value buckets, and consumers are created on startup. Session state is managed via JetStream KV, and the system adapts in real time to CLI-driven changes (e.g., new sessions, subjects, or commands).
- **Simulation Mode:** Runs a deterministic, side-effect-free scenario using a fresh, in-memory JetStream server. All state is discarded on exit, ensuring repeatable, isolated test runs.

The architecture is modular:
- `cmd/binrun/` – main entry point
- `internal/` – core business logic, simulation, config, and embedded server helpers
- `Taskfile.yml` – hot-reload and build tasks
- `docs/` – design notes, CLI/Go API reference, and how-tos

binrun is ideal for:
- Prototyping and learning JetStream (streams, KV, mirroring, transforms)
- Testing dynamic subscription and work-queue patterns
- Running repeatable, isolated simulations
- Interacting with a live JetStream system via the NATS CLI

---

## Usage & Workflow

### Repository layout

```
cmd/
└─ binrun/          # main entry-point (see below)
internal/
   ├─ core.go       # business logic + Sim implementation
   ├─ embedded_server.go  # helper to start an embedded NATS server
   ├─ config.go     # default configs + loader
   └─ types.go      # AppConfig, FlagsConfig, SimConfig, …
docs/               # design notes, scratch pads, how-tos
Taskfile.yml        # build / hot-reload / utility tasks
```

### Configuration model

```go
// internal/types.go

// ... existing code ...
```

Defaults are loaded from environment variables:

| ENV               | Purpose                            | Default |
|-------------------|------------------------------------|---------|
| `SIM`             | `true` → run the deterministic simulation instead of business logic | `false` |

All other settings (store dir, stream counts, etc.) are hard-coded defaults for now; extend `LoadAppConfig()` as needed.

### Taskfile commands

| Task              | What it does                                                             |
|-------------------|---------------------------------------------------------------------------|
| `task -w hot`     | Watches `*.go`/`*.templ`, rebuilds `bin/binrun`, kills the old process, runs the new one ( **business logic mode** ) |
| `task` *(default)*| Alias for `task -w hot`                                                   |
| `task tools`      | Installs Task, templ, goimports, NATS CLI, and NATS server binaries       |
| `task fmt`, `tidy`, `templ`, `build`, `kill` | Usual helpers                                  |

To run the simulation once (no watch), set the env var:

```bash
SIM=true task build   # or: SIM=true bin/binrun
```

### Quick start

```bash
# 1. Install Task and Go 1.22+
go install github.com/go-task/task/v3/cmd/task@latest

# 2. Start hot-reload dev loop
task        # or: task -w hot
# -> A NATS server starts in-process on each change.
#    Streams: COMMAND, EVENT, COMMANDX_MIRROR
#    KV bucket: sessions

# 3. Interact with it (in a separate shell)
nats pub event.orders.created '{"order_id":123,"status":"NEW"}'
nats kv put sessions Alice '{"subscriptions":["event.orders.created"]}'
```

#### Run the deterministic simulation

```bash
SIM=true bin/binrun
# or:
SIM=true task build
```

Simulation output looks like:

```
Sim: starting scenario with {NumSessions:10 ...}
Sim: environment reset complete
Sim: created 10 sessions
Sim: published 5 events per subject to 3 subjects
Sim: published 5 command.x messages
Sim: churned 2 sessions
Sim: mirror stream has correct message count: 5
Sim: session existence checks complete
Sim: scenario PASSED
```

Every run is isolated—streams and buckets live only in memory and are discarded when the program exits.

---

## Extending

* **More flags** – add fields to `FlagsConfig`, read from `os.Getenv` in `defaultFlagsCfg()`.
* **Scenario files** – parse YAML/JSON into `SimConfig` and call `core.Sim(...)`.
* **Additional streams** – edit `core.Run` (prod logic) and `core.Sim` (scenario) as desired.  
* **Persistent store** – set `EmbeddedServerConfig.StoreDir` and `EnableLogging`.  
* **CLI / HTTP metrics** – enable the NATS monitoring port in `embedded_server.go`.

---

## Messaging API

The application uses a centralized messaging schema defined in `internal/messages/` that provides type-safe message construction and validation for all NATS messaging.

### Message Types

All messages implement one of two interfaces:
- **Commands**: Input messages that request something to happen (e.g., `ScriptCreateCommand`)
- **Events**: Output messages that indicate something has happened (e.g., `ScriptCreatedEvent`)

### Available Messages

#### Script Domain

**Commands:**
- `ScriptCreateCommand` - Create a new script project
- `ScriptRunCommand` - Execute an existing script

**Events:**
- `ScriptCreatedEvent` - Script successfully created
- `ScriptCreateErrorEvent` - Script creation failed
- `ScriptJobStartedEvent` - Script job started execution
- `ScriptJobOutputEvent` - Script job produced stdout/stderr
- `ScriptJobExitEvent` - Script job completed
- `ScriptJobErrorEvent` - Script job failed to start

#### Terminal Domain

**Commands:**
- `TerminalCommandMessage` - Command entered in terminal

**Events:**
- `TerminalFreezeEvent` - Terminal output to display
- `TerminalViewDocEvent` - Document viewing triggered

### Usage Examples

```go
import "binrun/internal/messages"

// Create and publish a command
cmd := messages.NewScriptCreateCommand("my-script", "python").
    WithCorrelation("req-123")

publisher := messages.NewPublisher(js)
if err := publisher.PublishCommand(ctx, cmd); err != nil {
    log.Fatal(err)
}

// Create and publish an event
evt := messages.NewScriptCreatedEvent("my-script", "python").
    WithCorrelation("req-123")

if err := publisher.PublishEvent(ctx, evt); err != nil {
    log.Fatal(err)
}
```

### Subject Patterns

All NATS subject patterns are defined as constants in the schema:

```go
// Commands
messages.ScriptCreateSubject        // "command.script.create"
messages.ScriptRunSubjectPattern    // "command.script.*.run"

// Events
messages.ScriptCreatedSubjectPattern     // "event.script.*.created"
messages.ScriptJobStartedSubjectPattern  // "event.script.*.job.*.started"
messages.TerminalFreezeSubjectPattern    // "event.terminal.session.*.freeze"
```

Helper functions generate concrete subjects:

```go
messages.ScriptRunSubject("foo")           // "command.script.foo.run"
messages.ScriptJobExitSubject("foo", "42") // "event.script.foo.job.42.exit"
messages.TerminalFreezeSubject("abc123")   // "event.terminal.session.abc123.freeze"
```

### Validation

All commands and events include validation to ensure data integrity:

```go
cmd := messages.NewScriptCreateCommand("", "python") // Invalid: empty name
if err := cmd.Validate(); err != nil {
    // Error: script_name is required
}
```

The `Publisher` automatically validates messages before publishing, returning clear error messages for invalid data.

---

## Declarative Layout System

The application supports a declarative layout system for arranging subscription tiles within panels. Layouts are defined as JSON structures and stored in the session KV.

### Layout Node Types

1. **Leaf Node**: Displays a single subscription
```json
{"subscription": "event.cpu.host1.freeze"}
```

2. **Binary Split**: Divides space between two children
```json
{
  "split": "horizontal",  // or "vertical"
  "at": "2/3",           // fractions: "1/2", "1/3", "2/3", "1/4", "3/4"
  "first": {...},        // any node type
  "second": {...}        // any node type
}
```

3. **Even Split**: Divides space equally among N children
```json
{
  "split": "even-3",     // "even-2" through "even-5"
  "direction": "horizontal",  // or "vertical"
  "items": [{...}, {...}, {...}]
}
```

### Setting Layouts

Layouts are set via the session KV entry:

```bash
# Simple two-panel layout
nats kv put sessions $SESSION_ID '{
  "subscriptions": ["event.orders.*", "event.logs.*"],
  "layout": {
    "panels": {
      "main": {
        "split": "horizontal",
        "at": "2/3",
        "first": {"subscription": "event.orders.*"},
        "second": {"subscription": "event.logs.*"}
      }
    }
  }
}'

# Complex nested layout
nats kv put sessions $SESSION_ID '{
  "subscriptions": ["event.cpu.*", "event.memory.*", "event.disk.*", "event.logs.*"],
  "layout": {
    "panels": {
      "main": {
        "split": "horizontal",
        "at": "3/4",
        "first": {
          "split": "even-3",
          "direction": "horizontal",
          "items": [
            {"subscription": "event.cpu.*"},
            {"subscription": "event.memory.*"},
            {"subscription": "event.disk.*"}
          ]
        },
        "second": {"subscription": "event.logs.*"}
      }
    }
  }
}'
```

### Saving Layouts

Layouts can be saved to the `layouts` KV bucket for reuse:

```bash
# Save a layout
nats kv put layouts "my-dashboard" '{
  "panels": {
    "main": {
      "split": "vertical",
      "at": "1/2",
      "first": {"subscription": "event.metrics.*"},
      "second": {"subscription": "event.logs.*"}
    }
  }
}'

# List saved layouts
nats kv keys layouts

# Get a saved layout
nats kv get layouts my-dashboard
```

### Layout Behavior

- If no layout is specified, subscriptions are displayed in a simple grid
- Subscriptions in the layout must match those in the subscriptions array
- Invalid layouts fall back to grid display
- Only the `main` panel currently supports layouts (left, right, bottom panels can be added)

**Note**: Saved layouts are stored in the NATS KV bucket "layouts" (not yet implemented).

---

## Command Widgets

The declarative layout system supports interactive command forms that allow users to send typed NATS commands through the UI.

### Command Node Type

Command nodes render forms for sending messages:

```json
{
  "command": "ScriptCreateCommand",
  "defaults": {
    "script_type": "python"
  }
}
```

For script-specific commands, use the `script` field:

```json
{
  "command": "ScriptRunCommand",
  "script": "testbun",
  "defaults": {
    "args": "--verbose"
  }
}
```

### Supported Commands

Commands are automatically generated from the message schema using struct tags:

- **ScriptCreateCommand**: Create a new script project
  - `script_name` (required, text)
  - `script_type` (required, select: python/typescript)

- **ScriptRunCommand**: Run an existing script
  - Script name is specified in the layout `script` field
  - `args` (optional, text - space-separated)
  - `env` (optional, key-value pairs)

### Field Types

The system infers field types from Go struct tags:
- `field_type:"select"` with `options:"python,typescript"`
- `required:"true"` for required fields
- `placeholder:"my-script"` for placeholder text

### Example Layout with Commands

```json
{
  "layout": {
    "panels": {
      "left": {
        "split": "vertical",
        "at": "1/2",
        "first": {
          "command": "ScriptCreateCommand",
          "defaults": {"script_type": "python"}
        },
        "second": {
          "command": "ScriptRunCommand",
          "script": "myapp",
          "defaults": {"args": "--debug"}
        }
      }
    }
  }
}
```

This creates:
- Top left: Form to create new scripts (defaults to python)
- Bottom left: Form to run the "myapp" script specifically

### API

Commands are sent via POST to:
- `/command/execute` for general commands (with `_messageType` field)
- `/command/script/{name}/run` for script-specific run commands

The forms use Datastar's form handling with proper validation. All field values are bound to signals for reactive updates.

---

# Appendix: JetStream 80/20 CLI & Go API Reference

## NATS CLI – Streams, Consumers, KV, Object Store

### Streams
- **Create:** `nats stream add <StreamName> --subjects "<subject.pattern>" [--storage file|memory]`
- **List:** `nats stream ls` or `nats stream list -a`
- **Info:** `nats stream info <StreamName>`
- **View messages:** `nats stream view <StreamName>` or `nats stream get <StreamName> --seq <N>`
- **Remove/Purge:** `nats stream rm <StreamName>`, `nats stream purge <StreamName>`

### Consumers
- **Add:** `nats consumer add <Stream> <ConsumerName> [--filter <subject>] [--ack explicit|none|all] [--pull]`
- **List:** `nats consumer ls <Stream>`
- **Info:** `nats consumer info <Stream> <ConsumerName>`
- **Next message:** `nats consumer next <Stream> <ConsumerName> [--batch <N>]`
- **Delete:** `nats consumer rm <Stream> <ConsumerName>`

### Key-Value Store
- **Create bucket:** `nats kv add <BucketName> [--history <N>] [--ttl <duration>]`
- **List buckets:** `nats kv ls`
- **Put/Get:** `nats kv put <Bucket> <Key> <Value>`, `nats kv get <Bucket> <Key>`
- **Watch:** `nats kv watch <Bucket> [<KeyPattern>]`
- **History:** `nats kv history <Bucket> <Key>`
- **Delete:** `nats kv del <Bucket> <Key>`, `nats kv rm <Bucket>`

### Object Store
- **Create bucket:** `nats object add <BucketName>`
- **List buckets/objects:** `nats object ls`, `nats object ls <Bucket>`
- **Put/Get:** `nats object put <Bucket> <filePath>`, `nats object get <Bucket> <ObjectName> [--output <filePath>]`
- **Delete:** `nats object rm <Bucket> <ObjectName>`
- **Info:** `nats object info <Bucket> <ObjectName>`
- **Watch:** `nats object watch <Bucket>`

### JetStream Observability & Admin
- **Account info:** `nats account info`
- **Stream/Consumer health:** `nats server check stream`, `nats server check consumer`
- **Server stats:** `nats server info`, `nats server list`, `nats server report`
- **Publish/Subscribe:** `nats pub <subject> <message>`, `nats sub <subject>`, `nats sub --js <subject>`

---

## Go API – jetstream package (nats.go)

### Setup
```go
import (
    "context"
    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/jetstream"
)

ctx := context.Background()
nc, _ := nats.Connect(nats.DefaultURL)
js, _ := jetstream.New(nc)
```

### Streams
```go
// Create
s, _ := js.CreateStream(ctx, jetstream.StreamConfig{Name: "ORDERS", Subjects: []string{"ORDERS.*"}})
// Update
s, _ = js.UpdateStream(ctx, jetstream.StreamConfig{Name: "ORDERS", Subjects: []string{"ORDERS.*"}})
// Get handle
s, _ = js.Stream(ctx, "ORDERS")
// Delete
js.DeleteStream(ctx, "ORDERS")
```

### Consumers
```go
// Create durable
cons, _ := js.CreateConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{Durable: "foo", AckPolicy: jetstream.AckExplicitPolicy})
// Create ephemeral
cons, _ := js.CreateConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{AckPolicy: jetstream.AckExplicitPolicy})
// Update
updated, _ := js.UpdateConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{AckPolicy: jetstream.AckExplicitPolicy})
// Get handle
cons, _ = js.Consumer(ctx, "ORDERS", "foo")
// Delete
js.DeleteConsumer(ctx, "ORDERS", "foo")
```

### Message Consumption
```go
// Fetch batch
msgs, _ := cons.Fetch(10)
for msg := range msgs.Messages() { msg.Ack() }
// Callback
consCtx, _ := cons.Consume(func(msg jetstream.Msg) { msg.Ack() })
defer consCtx.Stop()
// Iterator
it, _ := cons.Messages()
for i := 0; i < 10; i++ { msg, _ := it.Next(); msg.Ack() }
it.Stop()
```

### Key-Value Store
```go
kv, _ := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "profiles"})
kv.Put(ctx, "sue.color", []byte("blue"))
entry, _ := kv.Get(ctx, "sue.color")
kv.Delete(ctx, "sue.color")
js.DeleteKeyValue(ctx, "profiles")
// Watch
watcher, _ := kv.Watch(ctx, "sue.*")
defer watcher.Stop()
entry = <-watcher.Updates()
```

### Object Store
```go
os, _ := js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{Bucket: "configs"})
os.PutString(ctx, "config-1", "first config")
os.Get(ctx, "config-1")
os.Delete(ctx, "config-1")
js.DeleteObjectStore(ctx, "configs")
// Watch
watcher, _ := os.Watch(ctx)
defer watcher.Stop()
object := <-watcher.Updates()
```

### Publishing
```go
ack, _ := js.Publish(ctx, "ORDERS.new", []byte("hello"))
ackF, _ := js.PublishAsync("ORDERS.new", []byte("hello"))
```

---

For more, see [NATS JetStream Go API Reference](https://pkg.go.dev/github.com/nats-io/nats.go/jetstream) and [NATS CLI Reference](https://docs.nats.io/).
