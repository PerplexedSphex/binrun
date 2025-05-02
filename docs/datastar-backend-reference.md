# Architecture Decision Record: Datastar SDK

## Summary

Datastar has had a few helper tools in the past for different languages.  The SDK effort is to unify around the tooling needed for Hypermedia On Whatever your Like (HOWL) based UIs.  Although Datastar the library can use any plugins, the default bundle includes robust Server Sent Event (SSE) base approach.  Most current languages and backend don't have great tooling around the style of delivering content to the frontend.

### Decision

Provide an SDK in a language agnostic way, to that end

1. Keep SDK as minimal as possible
2. Allow per language/framework extended features to live in an SDK ***sugar*** version

## Details

### Assumptions

The core mechanics of Datastar’s SSE support is

1. Data gets sent to browser as SSE events.
2. Data comes in via JSON from browser under a `datastar` namespace.

# Library

> [!WARNING] All naming conventions are shown using `Go` as the standard. Things may vary per language norms but please keep as close as possible.

## ServerSentEventGenerator

***There must*** be a `ServerSentEventGenerator` namespace.  In Go this is implemented as a struct, but could be a class or even namespace in languages such as C.

### Construction / Initialization
   1. ***There must*** be a way to create a new instance of this object based on the incoming `HTTP` Request and Response objects.
   2. The `ServerSentEventGenerator` ***must*** use a response controller that has the following response headers set by default
      1. `Cache-Control = nocache`
      2. `Content-Type = text/event-stream`
      3. `Connection = keep-alive` ***only*** if a HTTP/1.1 connection is used (see [spec](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Connection))
   3. Then the created response ***should*** `flush` immediately to avoid timeouts while 0-♾️ events are created
   4. Multiple calls using `ServerSentEventGenerator` should be single threaded to guarantee order.  The Go implementation uses a mutex to facilitate this behavior but might not be needed in a some environments

### `ServerSentEventGenerator.send`

```
ServerSentEventGenerator.send(
    eventType: EventType,
    dataLines: string[],
    options?: {
        eventId?: string,
        retryDuration?: durationInMilliseconds
    }
)
```

All top level `ServerSentEventGenerator` ***should*** use a unified sending function.  This method ***should be private/protected***

####  Args

##### EventType
An enum of Datastar supported events.  Will be a string over the wire.
Currently valid values are

| Event                     | Description                         |
|---------------------------|-------------------------------------|
| datastar-merge-fragments  | Merges HTML fragments into the DOM  |
| datastar-merge-signals    | Merges signals into the signals       |
| datastar-remove-fragments | Removes HTML fragments from the DOM |
| datastar-remove-signals   | Removes signals from the signals      |
| datastar-execute-script   | Executes JavaScript in the browser  |

##### Options
* `eventId` (string) Each event ***may*** include an `eventId`.  This can be used by the backend to replay events.  This is part of the SSE spec and is used to tell the browser how to handle the event.  For more details see https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events#id
* `retryDuration` (duration) Each event ***may*** include a `retryDuration` value.  If one is not provided the SDK ***must*** default to `1000` milliseconds.  This is part of the SSE spec and is used to tell the browser how long to wait before reconnecting if the connection is lost. For more details see https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events#retry

#### Logic
When called the function ***must*** write to the response buffer the following in specified order.  If any part of this process fails you ***must*** return/throw an error depending on language norms.
1. ***Must*** write `event: EVENT_TYPE\n` where `EVENT_TYPE` is [EventType](#EventType).
2. If a user defined event ID is provided, the function ***must*** write `id: EVENT_ID\n` where `EVENT_ID` is the event ID.
3. ***Must*** write `retry: RETRY_DURATION\n` where `RETRY_DURATION` is the provided retry duration, ***unless*** the value is the default of `1000` milliseconds.
4. For each string in the provided `dataLines`, you ***must*** write `data: DATA\n` where `DATA` is the provided string.
5. ***Must*** write a `\n\n` to complete the event per the SSE spec.
6. Afterward the writer ***should*** immediately flush.  This can be confounded by other middlewares such as compression layers.

### `ServerSentEventGenerator.MergeFragments`

```
ServerSentEventGenerator.MergeFragments(
    fragments: string,
    options?: {
        selector?: string,
        mergeMode?: FragmentMergeMode,
        useViewTransition?: boolean,
        eventId?: string,
        retryDuration?: durationInMilliseconds
     }
 )
```

#### Example Output

Minimal:

```
event: datastar-merge-fragments
data: fragments <div id="feed">
data: fragments     <span>1</span>
data: fragments </div>
```

Maximal:

```
event: datastar-merge-fragments
id: 123
retry: 2000
data: selector #feed
data: useViewTransition true
data: fragments <div id="feed">
data: fragments     <span>1</span>
data: fragments </div>
```

`MergeFragments` is a helper function to send HTML fragments to the browser to be merged into the DOM.

#### Args

##### FragmentMergeMode

An enum of Datastar supported fragment merge modes.  Will be a string over the wire
Valid values should match the [FragmentMergeMode](#FragmentMergeMode) and currently include

| Mode             | Description                                             |
|------------------|---------------------------------------------------------|
| morph            | Use Idiomorph to merge the fragment into the DOM        |
| inner            | Replace the innerHTML of the selector with the fragment |
| outer            | Replace the outerHTML of the selector with the fragment |
| prepend          | Prepend the fragment to the selector                    |
| append           | Append the fragment to the selector                     |
| before           | Insert the fragment before the selector                 |
| after            | Insert the fragment after the selector                  |
| upsertAttributes | Update the attributes of the selector with the fragment |

##### Options
* `selector` (string) The CSS selector to use to insert the fragments.  If not provided or empty, Datastar **will** default to using the `id` attribute of the fragment.
* `mergeMode` (FragmentMergeMode) The mode to use when merging the fragment into the DOM.  If not provided the Datastar client side ***will*** default to `morph`.
* `useViewTransition` Whether to use view transitions, if not provided the Datastar client side ***will*** default to `false`.

#### Logic
When called the function ***must*** call `ServerSentEventGenerator.send` with the `datastar-merge-fragments` event type.
1. If `selector` is provided, the function ***must*** include the selector in the event data in the format `selector SELECTOR\n`, ***unless*** the selector is empty.
2. If `mergeMode` is provided, the function ***must*** include the merge mode in the event data in the format `merge MERGE_MODE\n`, ***unless*** the value is the default of `morph`.
3. If `useViewTransition` is provided, the function ***must*** include the view transition in the event data in the format `useViewTransition USE_VIEW_TRANSITION\n`, ***unless*** the value is the default of `false`.  `USE_VIEW_TRANSITION` should be `true` or `false` (string), depending on the value of the `useViewTransition` option.
4. The function ***must*** include the fragments in the event data, with each line prefixed with `fragments `. This ***should*** be output after all other event data.

### `ServerSentEventGenerator.RemoveFragments`

```
ServerSentEventGenerator.RemoveFragments(
    selector: string,
    options?: {
        useViewTransition?: boolean,
        eventId?: string,
        retryDuration?: durationInMilliseconds
    }
)
```

#### Example Output

Minimal:

```
event: datastar-remove-fragments
data: selector #target
```

Maximal:

```
event: datastar-remove-fragments
id: 123
retry: 2000
data: selector #target
data: useViewTransition true
```

`RemoveFragments` is a helper function to send a selector to the browser to remove HTML fragments from the DOM.

#### Args

`selector` is a CSS selector that represents the fragments to be removed from the DOM.  The selector ***must*** be a valid CSS selector.  The Datastar client side will use this selector to remove the fragment from the DOM.

##### Options

* `useViewTransition` Whether to use view transitions, if not provided the Datastar client side ***will*** default to `false`.

#### Logic
1. When called the function ***must*** call `ServerSentEventGenerator.send` with the `datastar-remove-fragments` event type.
2. The function ***must*** include the selector in the event data in the format `selector SELECTOR\n`.
3. If `useViewTransition` is provided, the function ***must*** include the view transition in the event data in the format `useViewTransition USE_VIEW_TRANSITION\n`, ***unless*** the value is the default of `false`.  `USE_VIEW_TRANSITION` should be `true` or `false` (string), depending on the value of the `useViewTransition` option.


### `ServerSentEventGenerator.MergeSignals`

```
ServerSentEventGenerator.MergeSignals(
    signals: string,
    options ?: {
        onlyIfMissing?: boolean,
        eventId?: string,
        retryDuration?: durationInMilliseconds
     }
 )
```

#### Example Output

Minimal:

```
event: datastar-merge-signals
data: signals {"output":"Patched Output Test","show":true,"input":"Test","user":{"name":"","email":""}}
```

Maximal:

```
event: datastar-merge-signals
id: 123
retry: 2000
data: onlyIfMissing true
data: signals {"output":"Patched Output Test","show":true,"input":"Test","user":{"name":"","email":""}}
```

`MergeSignals` is a helper function to send one or more signals to the browser to be merged into the signals.

#### Args

Data is a JavaScript object or JSON string that will be sent to the browser to update signals in the signals.  The data ***must*** evaluate to a valid JavaScript.  It will be converted to signals by the Datastar client side.

##### Options

* `onlyIfMissing` (boolean) Whether to merge the signal only if it does not already exist.  If not provided, the Datastar client side ***will*** default to `false`, which will cause the data to be merged into the signals.

#### Logic
When called the function ***must*** call `ServerSentEventGenerator.send` with the `datastar-merge-signals` event type.

1. If `onlyIfMissing` is provided, the function ***must*** include it in the event data in the format `onlyIfMissing ONLY_IF_MISSING\n`, ***unless*** the value is the default of `false`.  `ONLY_IF_MISSING` should be `true` or `false` (string), depending on the value of the `onlyIfMissing` option.
2. The function ***must*** include the signals in the event data, with each line prefixed with `signals `.  This ***should*** be output after all other event data.

### `ServerSentEventGenerator.RemoveSignals`

```html
ServerSentEventGenerator.RemoveSignals(
    paths: string[],
    options?: {
        eventId?: string,
        retryDuration?: durationInMilliseconds
    }
)
```

#### Example Output

Minimal:

```
event: datastar-remove-signals
data: paths user.name
data: paths user.email
```

Maximal:

```
event: datastar-remove-signals
id: 123
retry: 2000
data: paths user.name
data: paths user.email
```

`RemoveSignals` is a helper function to send signals to the browser to be removed from the signals.

#### Args

`paths` is a list of strings that represent the signal paths to be removed from the signals.  The paths ***must*** be valid `.` delimited paths to signals within the signals.  The Datastar client side will use these paths to remove the data from the signals.

#### Logic
When called the function ***must*** call `ServerSentEventGenerator.send` with the `datastar-remove-signals` event type.

1. The function ***must*** include the paths in the event data, with each line prefixed with `paths `.  Space-separated paths such as `paths foo.bar baz.qux.hello world` ***should*** be allowed.

### `ServerSentEventGenerator.ExecuteScript`

```
ServerSentEventGenerator.ExecuteScript(
    script: string,
    options?: {
        autoRemove?: boolean,
        attributes?: string,
        eventId?: string,
        retryDuration?: durationInMilliseconds
    }
)
```

#### Example Output

Minimal:

```
event: datastar-execute-script
data: script window.location = "https://data-star.dev"
```

Maximal:

```
event: datastar-execute-script
id: 123
retry: 2000
data: autoRemove false
data: attributes type text/javascript
data: script window.location = "https://data-star.dev"
```

#### Args

`script` is a string that represents the JavaScript to be executed by the browser.

##### Options

* `autoRemove` Whether to remove the script after execution, if not provided the Datastar client side ***will*** default to `true`.
* `attributes` A line separated list of attributes to add to the `script` element, if not provided the Datastar client side ***will*** default to `type module`. Each item in the array should be a string in the format `key value`.

#### Logic
When called the function ***must*** call `ServerSentEventGenerator.send` with the `datastar-execute-script` event type.

1. If `autoRemove` is provided, the function ***must*** include the auto remove script value in the event data in the format `autoRemove AUTO_REMOVE\n`, ***unless*** the value is the default of `true`.
2. If `attributes` is provided, the function ***must*** include the attributes in the event data, with each line prefixed with `attributes `, ***unless*** the attributes value is the default of `type module`.
3. The function ***must*** include the script in the event data, with each line prefixed with `script `.  This ***should*** be output after all other event data.

## `ReadSignals(r *http.Request, signals any) error`

`ReadSignals` is a helper function to parse incoming data from the browser.  It should take the incoming request and convert into an object that can be used by the backend.

#### Args

* `r` (http.Request) The incoming request object from the browser.  This object ***must*** be a valid Request object per the language specifics.
* `signals` (any) The signals object that the incoming data will be extracted into.  The exact function signature will depend on the language specifics.

#### Logic

1. The function ***must*** parse the incoming HTTP request
   1. If the incoming method is `GET`, the function ***must*** parse the query string's `datastar` key and treat it as a URL encoded JSON string.
   2. Otherwise, the function ***must*** parse the body of the request as a JSON encoded string.
   3. If the incoming data is not valid JSON, the function ***must*** return an error.

# Go SDK for Datastar

[![Go
Reference](https://pkg.go.dev/badge/github.com/starfederation/datastar.svg)](https://pkg.go.dev/github.com/starfederation/datastar)

Implements the [SDK spec](../README.md) and exposes an abstract
ServerSentEventGenerator struct that can be used to implement runtime specific
classes.

Usage is straightforward:

```go
package main

import (
"crypto/rand"
"encoding/hex"
"fmt"
"log/slog"
"net/http"
"os"
"time"

datastar "github.com/starfederation/datastar/sdk/go"
)

const port = 9001

func main() {
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
mux := http.NewServeMux()

cdn := "https://cdn.jsdelivr.net/gh/starfederation/datastar@develop/bundles/datastar.js"
style := "display:flex;flex-direction:column;background-color:oklch(25.3267% 0.015896
252.417568);height:100vh;justify-content:center;align-items:center;font-family:ui-sans-serif, system-ui, sans-serif,
'Apple Color Emoji', 'Segoe UI Emoji', 'Segoe UI Symbol', 'Noto Color Emoji';"

page := []byte(fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">

<head>
	<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=0" />
	<script type="module" defer src="%s"></script>
</head>

<body style="%s">
	<span id="feed" data-on-load="%s"></span>
</body>

</html>
`, cdn, style, datastar.GetSSE("/stream")))

mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
w.Write(page)
})

mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) {
ticker := time.NewTicker(100 * time.Millisecond)
defer ticker.Stop()

sse := datastar.NewSSE(w, r)

for {
select {

case <-r.Context().Done(): logger.Debug("Client connection closed") return case <-ticker.C: bytes :=make([]byte, 3) _,
	err :=rand.Read(bytes) if err !=nil { logger.Error("Error generating random bytes: ", slog.String(" error",
	err.Error())) return } hexString :=hex.EncodeToString(bytes) frag :=fmt.Sprintf(`<span id="feed"
	style="color:#%s;border:1px solid #%s;border-radius:0.25rem;padding:1rem;">%s</span>`, hexString, hexString,
	hexString)

	sse.MergeFragments(frag)
	}
	}
	})

	logger.Info(fmt.Sprintf("Server starting at 0.0.0.0:%d", port))
	if err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), mux); err != nil {
	logger.Error("Error starting server:", slog.String("error", err.Error()))
	}

	}
	```

	## Examples

	The [Datastar website](https://data-star.dev) acts as a [set of
	examples](https://github.com/starfederation/datastar/tree/develop/site) for how to use the SDK.

    # Backend SDK

## types.go

```go
package datastar

import (
	"errors"
)

const (
	NewLine       = "\n"
	DoubleNewLine = "\n\n"
)

var (
	ErrEventTypeError = errors.New("event type is required")

	newLineBuf       = []byte(NewLine)
	doubleNewLineBuf = []byte(DoubleNewLine)
)

type flusher interface {
	Flush() error
}
```

## consts.go

```go
// This is auto-generated by Datastar. DO NOT EDIT.

package datastar

import "time"

const (
    DatastarKey = "datastar"
    Version                   = "1.0.0-beta.11"
    VersionClientByteSize     = 40026
    VersionClientByteSizeGzip = 14900

    //region Default durations

    // The default duration for retrying SSE on connection reset. This is part of the underlying retry mechanism of SSE.
    DefaultSseRetryDuration = 1000 * time.Millisecond

    //endregion Default durations

    //region Default strings

    // The default attributes for <script/> element use when executing scripts. It is a set of key-value pairs delimited by a newline \\n character.
    DefaultExecuteScriptAttributes = "type module"

    //endregion Default strings

    //region Dataline literals
    SelectorDatalineLiteral = "selector "
    MergeModeDatalineLiteral = "mergeMode "
    FragmentsDatalineLiteral = "fragments "
    UseViewTransitionDatalineLiteral = "useViewTransition "
    SignalsDatalineLiteral = "signals "
    OnlyIfMissingDatalineLiteral = "onlyIfMissing "
    PathsDatalineLiteral = "paths "
    ScriptDatalineLiteral = "script "
    AttributesDatalineLiteral = "attributes "
    AutoRemoveDatalineLiteral = "autoRemove "
    //endregion Dataline literals
)

var (
    //region Default booleans

    // Should fragments be merged using the ViewTransition API?
    DefaultFragmentsUseViewTransitions = false

    // Should a given set of signals merge if they are missing?
    DefaultMergeSignalsOnlyIfMissing = false

    // Should script element remove itself after execution?
    DefaultExecuteScriptAutoRemove = true

    //endregion Default booleans
)

//region Enums

//region The mode in which a fragment is merged into the DOM.
type FragmentMergeMode string

const (
    // Default value for FragmentMergeMode
    // Morphs the fragment into the existing element using idiomorph.
    DefaultFragmentMergeMode = FragmentMergeModeMorph

    // Morphs the fragment into the existing element using idiomorph.
    FragmentMergeModeMorph FragmentMergeMode = "morph"

    // Replaces the inner HTML of the existing element.
    FragmentMergeModeInner FragmentMergeMode = "inner"

    // Replaces the outer HTML of the existing element.
    FragmentMergeModeOuter FragmentMergeMode = "outer"

    // Prepends the fragment to the existing element.
    FragmentMergeModePrepend FragmentMergeMode = "prepend"

    // Appends the fragment to the existing element.
    FragmentMergeModeAppend FragmentMergeMode = "append"

    // Inserts the fragment before the existing element.
    FragmentMergeModeBefore FragmentMergeMode = "before"

    // Inserts the fragment after the existing element.
    FragmentMergeModeAfter FragmentMergeMode = "after"

    // Upserts the attributes of the existing element.
    FragmentMergeModeUpsertAttributes FragmentMergeMode = "upsertAttributes"

)
//endregion FragmentMergeMode

//region The type protocol on top of SSE which allows for core pushed based communication between the server and the client.
type EventType string

const (
    // An event for merging HTML fragments into the DOM.
    EventTypeMergeFragments EventType = "datastar-merge-fragments"

    // An event for merging signals.
    EventTypeMergeSignals EventType = "datastar-merge-signals"

    // An event for removing HTML fragments from the DOM.
    EventTypeRemoveFragments EventType = "datastar-remove-fragments"

    // An event for removing signals.
    EventTypeRemoveSignals EventType = "datastar-remove-signals"

    // An event for executing <script/> elements in the browser.
    EventTypeExecuteScript EventType = "datastar-execute-script"

)
//endregion EventType

//endregion Enums
```

## sse.go

```go
package datastar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/valyala/bytebufferpool"
)

type ServerSentEventGenerator struct {
	ctx             context.Context
	mu              *sync.Mutex
	w               io.Writer
	rc              *http.ResponseController
	shouldLogPanics bool
	encoding        string
	acceptEncoding  string
}

type SSEOption func(*ServerSentEventGenerator)

func NewSSE(w http.ResponseWriter, r *http.Request, opts ...SSEOption) *ServerSentEventGenerator {
	rc := http.NewResponseController(w)

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/event-stream")
	if r.ProtoMajor == 1 {
		w.Header().Set("Connection", "keep-alive")
	}

	sseHandler := &ServerSentEventGenerator{
		ctx:             r.Context(),
		mu:              &sync.Mutex{},
		w:               w,
		rc:              rc,
		shouldLogPanics: true,
		acceptEncoding:  r.Header.Get("Accept-Encoding"),
	}

	// Apply options
	for _, opt := range opts {
		opt(sseHandler)
	}

	// set compression encoding
	if sseHandler.encoding != "" {
		w.Header().Set("Content-Encoding", sseHandler.encoding)
	}

	// flush headers
	if err := rc.Flush(); err != nil {
		// Below panic is a deliberate choice as it should never occur and is an environment issue.
		// https://crawshaw.io/blog/go-and-sqlite
		// In Go, errors that are part of the standard operation of a program are returned as values.
		// Programs are expected to handle errors.
		panic(fmt.Sprintf("response writer failed to flush: %v", err))
	}

	return sseHandler
}

func (sse *ServerSentEventGenerator) Context() context.Context {
	return sse.ctx
}

type ServerSentEventData struct {
	Type          EventType
	EventID       string
	Data          []string
	RetryDuration time.Duration
}

type SSEEventOption func(*ServerSentEventData)

func WithSSEEventId(id string) SSEEventOption {
	return func(e *ServerSentEventData) {
		e.EventID = id
	}
}

func WithSSERetryDuration(retryDuration time.Duration) SSEEventOption {
	return func(e *ServerSentEventData) {
		e.RetryDuration = retryDuration
	}
}

var (
	eventLinePrefix = []byte("event: ")
	idLinePrefix    = []byte("id: ")
	retryLinePrefix = []byte("retry: ")
	dataLinePrefix  = []byte("data: ")
)

func writeJustError(w io.Writer, b []byte) (err error) {
	_, err = w.Write(b)
	return err
}

func (sse *ServerSentEventGenerator) Send(eventType EventType, dataLines []string, opts ...SSEEventOption) error {
	sse.mu.Lock()
	defer sse.mu.Unlock()

	// create the event
	evt := ServerSentEventData{
		Type:          eventType,
		Data:          dataLines,
		RetryDuration: DefaultSseRetryDuration,
	}

	// apply options
	for _, opt := range opts {
		opt(&evt)
	}

	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	// write event type
	if err := errors.Join(
		writeJustError(buf, eventLinePrefix),
		writeJustError(buf, []byte(evt.Type)),
		writeJustError(buf, newLineBuf),
	); err != nil {
		return fmt.Errorf("failed to write event type: %w", err)
	}

	// write id if needed
	if evt.EventID != "" {
		if err := errors.Join(
			writeJustError(buf, idLinePrefix),
			writeJustError(buf, []byte(evt.EventID)),
			writeJustError(buf, newLineBuf),
		); err != nil {
			return fmt.Errorf("failed to write id: %w", err)
		}
	}

	// write retry if needed
	if evt.RetryDuration.Milliseconds() > 0 && evt.RetryDuration.Milliseconds() != DefaultSseRetryDuration.Milliseconds() {
		retry := int(evt.RetryDuration.Milliseconds())
		retryStr := strconv.Itoa(retry)
		if err := errors.Join(
			writeJustError(buf, retryLinePrefix),
			writeJustError(buf, []byte(retryStr)),
			writeJustError(buf, newLineBuf),
		); err != nil {
			return fmt.Errorf("failed to write retry: %w", err)
		}
	}

	// write data lines
	for _, d := range evt.Data {
		if err := errors.Join(
			writeJustError(buf, dataLinePrefix),
			writeJustError(buf, []byte(d)),
			writeJustError(buf, newLineBuf),
		); err != nil {
			return fmt.Errorf("failed to write data: %w", err)
		}
	}

	// write double newlines to separate events
	if err := writeJustError(buf, doubleNewLineBuf); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// copy the buffer to the response writer
	if _, err := buf.WriteTo(sse.w); err != nil {
		return fmt.Errorf("failed to write to response writer: %w", err)
	}

	// flush the write if its a compressing writer
	if f, ok := sse.w.(flusher); ok {
		if err := f.Flush(); err != nil {
			return fmt.Errorf("failed to flush compressing writer: %w", err)
		}
	}

	if err := sse.rc.Flush(); err != nil {
		return fmt.Errorf("failed to flush data: %w", err)
	}

	// log.Print(NewLine + buf.String())

	return nil
}
```

## sse-compression.go

```go
package datastar

import (
	"strings"

	"github.com/CAFxX/httpcompression/contrib/andybalholm/brotli"
	"github.com/CAFxX/httpcompression/contrib/compress/gzip"
	"github.com/CAFxX/httpcompression/contrib/compress/zlib"
	"github.com/CAFxX/httpcompression/contrib/klauspost/zstd"
	zstd_opts "github.com/klauspost/compress/zstd"

	"github.com/CAFxX/httpcompression"
)

type CompressionStrategy string

const (
	ClientPriority = "client_priority"
	ServerPriority = "server_priority"
	Forced         = "forced"
)

type Compressor struct {
	Encoding   string
	Compressor httpcompression.CompressorProvider
}

type CompressionConfig struct {
	CompressionStrategy CompressionStrategy
	ClientEncodings     []string
	Compressors         []Compressor
}

type CompressionOption func(*CompressionConfig)

type GzipOption func(*gzip.Options)

func WithGzipLevel(level int) GzipOption {
	return func(opts *gzip.Options) {
		opts.Level = level
	}
}

func WithGzip(opts ...GzipOption) CompressionOption {
	return func(cfg *CompressionConfig) {
		// set default options
		options := gzip.Options{
			Level: gzip.DefaultCompression,
		}
		// Apply all provided options.
		for _, opt := range opts {
			opt(&options)
		}

		gzipCompressor, _ := gzip.New(options)

		compressor := Compressor{
			Encoding:   gzip.Encoding,
			Compressor: gzipCompressor,
		}

		cfg.Compressors = append(cfg.Compressors, compressor)

	}
}

type DeflateOption func(*zlib.Options)

func WithDeflateLevel(level int) DeflateOption {
	return func(opts *zlib.Options) {
		opts.Level = level
	}
}

func WithDeflateDictionary(dict []byte) DeflateOption {
	return func(opts *zlib.Options) {
		opts.Dictionary = dict
	}
}

func WithDeflate(opts ...DeflateOption) CompressionOption {
	return func(cfg *CompressionConfig) {
		options := zlib.Options{
			Level: zlib.DefaultCompression,
		}

		for _, opt := range opts {
			opt(&options)
		}

		zlibCompressor, _ := zlib.New(options)

		compressor := Compressor{
			Encoding:   zlib.Encoding,
			Compressor: zlibCompressor,
		}

		cfg.Compressors = append(cfg.Compressors, compressor)
	}
}

type brotliOption func(*brotli.Options)

func WithBrotliLevel(level int) brotliOption {
	return func(opts *brotli.Options) {
		opts.Quality = level
	}
}

func WithBrotliLGWin(lgwin int) brotliOption {
	return func(opts *brotli.Options) {
		opts.LGWin = lgwin
	}
}

func WithBrotli(opts ...brotliOption) CompressionOption {
	return func(cfg *CompressionConfig) {
		options := brotli.Options{
			Quality: brotli.DefaultCompression,
		}

		for _, opt := range opts {
			opt(&options)
		}

		brotliCompressor, _ := brotli.New(options)

		compressor := Compressor{
			Encoding:   brotli.Encoding,
			Compressor: brotliCompressor,
		}

		cfg.Compressors = append(cfg.Compressors, compressor)
	}
}

func WithZstd(opts ...zstd_opts.EOption) CompressionOption {
	return func(cfg *CompressionConfig) {

		zstdCompressor, _ := zstd.New(opts...)

		compressor := Compressor{
			Encoding:   zstd.Encoding,
			Compressor: zstdCompressor,
		}

		cfg.Compressors = append(cfg.Compressors, compressor)
	}
}

func WithClientPriority() CompressionOption {
	return func(cfg *CompressionConfig) {
		cfg.CompressionStrategy = ClientPriority
	}
}

func WithServerPriority() CompressionOption {
	return func(cfg *CompressionConfig) {
		cfg.CompressionStrategy = ServerPriority
	}
}

func WithForced() CompressionOption {
	return func(cfg *CompressionConfig) {
		cfg.CompressionStrategy = Forced
	}
}

func WithCompression(opts ...CompressionOption) SSEOption {

	return func(sse *ServerSentEventGenerator) {
		cfg := &CompressionConfig{
			CompressionStrategy: ClientPriority,
			ClientEncodings:     parseEncodings(sse.acceptEncoding),
		}

		// apply options
		for _, opt := range opts {
			opt(cfg)
		}

		// set defaults
		if len(cfg.Compressors) == 0 {
			WithBrotli()(cfg)
			WithZstd()(cfg)
			WithGzip()(cfg)
			WithDeflate()(cfg)
		}

		switch cfg.CompressionStrategy {
		case ClientPriority:
			for _, clientEnc := range cfg.ClientEncodings {
				for _, comp := range cfg.Compressors {
					if comp.Encoding == clientEnc {
						sse.w = comp.Compressor.Get(sse.w)
						sse.encoding = comp.Encoding
						return
					}
				}
			}
		case ServerPriority:
			for _, comp := range cfg.Compressors {
				for _, clientEnc := range cfg.ClientEncodings {
					if comp.Encoding == clientEnc {
						sse.w = comp.Compressor.Get(sse.w)
						sse.encoding = comp.Encoding
						return
					}
				}
			}
		case Forced:
			if len(cfg.Compressors) > 0 {
				sse.w = cfg.Compressors[0].Compressor.Get(sse.w)
				sse.encoding = cfg.Compressors[0].Encoding
			}
		}
	}
}

func parseEncodings(header string) []string {
	parts := strings.Split(header, ",")
	var tokens []string
	for _, part := range parts {
		token := strings.SplitN(strings.TrimSpace(part), ";", 2)[0]
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}
```

## fragments.go

```go
package datastar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type MergeFragmentOptions struct {
	EventID            string
	RetryDuration      time.Duration
	Selector           string
	MergeMode          FragmentMergeMode
	UseViewTransitions bool
}

type MergeFragmentOption func(*MergeFragmentOptions)

func WithSelectorf(selectorFormat string, args ...any) MergeFragmentOption {
	selector := fmt.Sprintf(selectorFormat, args...)
	return WithSelector(selector)
}

func WithSelector(selector string) MergeFragmentOption {
	return func(o *MergeFragmentOptions) {
		o.Selector = selector
	}
}

func WithMergeMode(merge FragmentMergeMode) MergeFragmentOption {
	return func(o *MergeFragmentOptions) {
		o.MergeMode = merge
	}
}

func WithUseViewTransitions(useViewTransition bool) MergeFragmentOption {
	return func(o *MergeFragmentOptions) {
		o.UseViewTransitions = useViewTransition
	}
}

func (sse *ServerSentEventGenerator) MergeFragments(fragment string, opts ...MergeFragmentOption) error {
	options := &MergeFragmentOptions{
		EventID:        "",
		RetryDuration:  DefaultSseRetryDuration,
		Selector:       "",
		MergeMode:      FragmentMergeModeMorph,
	}
	for _, opt := range opts {
		opt(options)
	}

	sendOptions := make([]SSEEventOption, 0, 2)
	if options.EventID != "" {
		sendOptions = append(sendOptions, WithSSEEventId(options.EventID))
	}
	if options.RetryDuration > 0 {
		sendOptions = append(sendOptions, WithSSERetryDuration(options.RetryDuration))
	}

	dataRows := make([]string, 0, 4)
	if options.Selector != "" {
		dataRows = append(dataRows, SelectorDatalineLiteral+options.Selector)
	}
	if options.MergeMode != FragmentMergeModeMorph {
		dataRows = append(dataRows, MergeModeDatalineLiteral+string(options.MergeMode))
	}
	if options.UseViewTransitions {
		dataRows = append(dataRows, UseViewTransitionDatalineLiteral+"true")
	}

	if fragment != "" {
		parts := strings.Split(fragment, "\n")
		for _, part := range parts {
			dataRows = append(dataRows, FragmentsDatalineLiteral+part)
		}
	}

	if err := sse.Send(
		EventTypeMergeFragments,
		dataRows,
		sendOptions...,
	); err != nil {
		return fmt.Errorf("failed to send fragment: %w", err)
	}

	return nil
}

type RemoveFragmentsOptions struct {
	EventID            string
	RetryDuration      time.Duration
	UseViewTransitions *bool
}

type RemoveFragmentsOption func(*RemoveFragmentsOptions)

func WithRemoveEventID(id string) RemoveFragmentsOption {
	return func(o *RemoveFragmentsOptions) {
		o.EventID = id
	}
}

func WithRemoveRetryDuration(d time.Duration) RemoveFragmentsOption {
	return func(o *RemoveFragmentsOptions) {
		o.RetryDuration = d
	}
}

func WithRemoveUseViewTransitions(useViewTransition bool) RemoveFragmentsOption {
	return func(o *RemoveFragmentsOptions) {
		o.UseViewTransitions = &useViewTransition
	}
}

func (sse *ServerSentEventGenerator) RemoveFragments(selector string, opts ...RemoveFragmentsOption) error {
	if selector == "" {
		panic("missing " + SelectorDatalineLiteral)
	}

	options := &RemoveFragmentsOptions{
		EventID:            "",
		RetryDuration:      DefaultSseRetryDuration,
		UseViewTransitions: nil,
	}
	for _, opt := range opts {
		opt(options)
	}

	dataRows := []string{SelectorDatalineLiteral + selector}
	if options.UseViewTransitions != nil {
		dataRows = append(dataRows, UseViewTransitionDatalineLiteral+strconv.FormatBool(*options.UseViewTransitions))
	}

	sendOptions := make([]SSEEventOption, 0, 2)
	if options.EventID != "" {
		sendOptions = append(sendOptions, WithSSEEventId(options.EventID))
	}
	if options.RetryDuration > 0 {
		sendOptions = append(sendOptions, WithSSERetryDuration(options.RetryDuration))
	}

	if err := sse.Send(EventTypeRemoveFragments, dataRows, sendOptions...); err != nil {
		return fmt.Errorf("failed to send remove: %w", err)
	}
	return nil
}
```

## fragments-sugar.go 

```go
package datastar

import (
	"fmt"

	"github.com/a-h/templ"
	"github.com/delaneyj/gostar/elements"
	"github.com/valyala/bytebufferpool"
)

var ValidFragmentMergeTypes = []FragmentMergeMode{
	FragmentMergeModeMorph,
	FragmentMergeModeInner,
	FragmentMergeModeOuter,
	FragmentMergeModePrepend,
	FragmentMergeModeAppend,
	FragmentMergeModeBefore,
	FragmentMergeModeAfter,
	FragmentMergeModeUpsertAttributes,
}

func FragmentMergeTypeFromString(s string) (FragmentMergeMode, error) {
	for _, t := range ValidFragmentMergeTypes {
		if string(t) == s {
			return t, nil
		}
	}
	return "", fmt.Errorf("invalid fragment merge type: %s", s)
}

func WithMergeMorph() MergeFragmentOption {
	return WithMergeMode(FragmentMergeModeMorph)
}

func WithMergeInner() MergeFragmentOption {
	return WithMergeMode(FragmentMergeModeInner)
}

func WithMergeOuter() MergeFragmentOption {
	return WithMergeMode(FragmentMergeModeOuter)
}

func WithMergePrepend() MergeFragmentOption {
	return WithMergeMode(FragmentMergeModePrepend)
}

func WithMergeAppend() MergeFragmentOption {
	return WithMergeMode(FragmentMergeModeAppend)
}

func WithMergeBefore() MergeFragmentOption {
	return WithMergeMode(FragmentMergeModeBefore)
}

func WithMergeAfter() MergeFragmentOption {
	return WithMergeMode(FragmentMergeModeAfter)
}

func WithMergeUpsertAttributes() MergeFragmentOption {
	return WithMergeMode(FragmentMergeModeUpsertAttributes)
}

func WithSelectorID(id string) MergeFragmentOption {
	return WithSelector("#" + id)
}

func WithViewTransitions() MergeFragmentOption {
	return func(o *MergeFragmentOptions) {
		o.UseViewTransitions = true
	}
}

func WithoutViewTransitions() MergeFragmentOption {
	return func(o *MergeFragmentOptions) {
		o.UseViewTransitions = false
	}
}

func (sse *ServerSentEventGenerator) MergeFragmentf(format string, args ...any) error {
	return sse.MergeFragments(fmt.Sprintf(format, args...))
}

func (sse *ServerSentEventGenerator) MergeFragmentTempl(c templ.Component, opts ...MergeFragmentOption) error {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)
	if err := c.Render(sse.Context(), buf); err != nil {
		return fmt.Errorf("failed to merge fragment: %w", err)
	}
	if err := sse.MergeFragments(buf.String(), opts...); err != nil {
		return fmt.Errorf("failed to merge fragment: %w", err)
	}
	return nil
}

func (sse *ServerSentEventGenerator) MergeFragmentGostar(child elements.ElementRenderer, opts ...MergeFragmentOption) error {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)
	if err := child.Render(buf); err != nil {
		return fmt.Errorf("failed to render: %w", err)
	}
	if err := sse.MergeFragments(buf.String(), opts...); err != nil {
		return fmt.Errorf("failed to merge fragment: %w", err)
	}
	return nil
}

func GetSSE(urlFormat string, args ...any) string {
	return fmt.Sprintf(`@get('%s')`, fmt.Sprintf(urlFormat, args...))
}

func PostSSE(urlFormat string, args ...any) string {
	return fmt.Sprintf(`@post('%s')`, fmt.Sprintf(urlFormat, args...))
}

func PutSSE(urlFormat string, args ...any) string {
	return fmt.Sprintf(`@put('%s')`, fmt.Sprintf(urlFormat, args...))
}

func PatchSSE(urlFormat string, args ...any) string {
	return fmt.Sprintf(`@patch('%s')`, fmt.Sprintf(urlFormat, args...))
}

func DeleteSSE(urlFormat string, args ...any) string {
	return fmt.Sprintf(`@delete('%s')`, fmt.Sprintf(urlFormat, args...))
}
```

## signals.go

```go
package datastar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/valyala/bytebufferpool"
)

var (
	ErrNoPathsProvided = errors.New("no paths provided")
)

type MergeSignalsOptions struct {
	EventID       string
	RetryDuration time.Duration
	OnlyIfMissing bool
}

type MergeSignalsOption func(*MergeSignalsOptions)

func WithMergeSignalsEventID(id string) MergeSignalsOption {
	return func(o *MergeSignalsOptions) {
		o.EventID = id
	}
}

func WithMergeSignalsRetryDuration(retryDuration time.Duration) MergeSignalsOption {
	return func(o *MergeSignalsOptions) {
		o.RetryDuration = retryDuration
	}
}

func WithOnlyIfMissing(onlyIfMissing bool) MergeSignalsOption {
	return func(o *MergeSignalsOptions) {
		o.OnlyIfMissing = onlyIfMissing
	}
}

func (sse *ServerSentEventGenerator) MergeSignals(signalsContents []byte, opts ...MergeSignalsOption) error {
	options := &MergeSignalsOptions{
		EventID:       "",
		RetryDuration: DefaultSseRetryDuration,
		OnlyIfMissing: false,
	}
	for _, opt := range opts {
		opt(options)
	}

	dataRows := make([]string, 0, 32)
	if options.OnlyIfMissing {
		dataRows = append(dataRows, OnlyIfMissingDatalineLiteral+strconv.FormatBool(options.OnlyIfMissing))
	}
	lines := bytes.Split(signalsContents, newLineBuf)
	for _, line := range lines {
		dataRows = append(dataRows, SignalsDatalineLiteral+string(line))
	}

	sendOptions := make([]SSEEventOption, 0, 2)
	if options.EventID != "" {
		sendOptions = append(sendOptions, WithSSEEventId(options.EventID))
	}
	if options.RetryDuration != DefaultSseRetryDuration {
		sendOptions = append(sendOptions, WithSSERetryDuration(options.RetryDuration))
	}

	if err := sse.Send(
		EventTypeMergeSignals,
		dataRows,
		sendOptions...,
	); err != nil {
		return fmt.Errorf("failed to send merge signals: %w", err)
	}
	return nil
}

func (sse *ServerSentEventGenerator) RemoveSignals(paths ...string) error {
	if len(paths) == 0 {
		return ErrNoPathsProvided
	}

	dataRows := make([]string, 0, len(paths))
	for _, path := range paths {
		dataRows = append(dataRows, PathsDatalineLiteral+path)
	}

	if err := sse.Send(
		EventTypeRemoveSignals,
		dataRows,
	); err != nil {
		return fmt.Errorf("failed to send remove signals: %w", err)
	}
	return nil
}

func ReadSignals(r *http.Request, signals any) error {
	var dsInput []byte

	if r.Method == "GET" {
		dsJSON := r.URL.Query().Get(DatastarKey)
		if dsJSON == "" {
			return nil
		} else {
			dsInput = []byte(dsJSON)
		}
	} else {
		buf := bytebufferpool.Get()
		defer bytebufferpool.Put(buf)
		if _, err := buf.ReadFrom(r.Body); err != nil {
			if err == http.ErrBodyReadAfterClose {
				return fmt.Errorf("body already closed, are you sure you created the SSE ***AFTER*** the ReadSignals? %w", err)
			}
			return fmt.Errorf("failed to read body: %w", err)
		}
		dsInput = buf.Bytes()
	}

	if err := json.Unmarshal(dsInput, signals); err != nil {
		return fmt.Errorf("failed to unmarshal: %w", err)
	}
	return nil
}
```

## signals-sugar.go

```go
package datastar

import (
	"encoding/json"
	"fmt"
)

func (sse *ServerSentEventGenerator) MarshalAndMergeSignals(signals any, opts ...MergeSignalsOption) error {
	b, err := json.Marshal(signals)
	if err != nil {
		panic(err)
	}
	if err := sse.MergeSignals(b, opts...); err != nil {
		return fmt.Errorf("failed to merge signals: %w", err)
	}

	return nil
}

func (sse *ServerSentEventGenerator) MarshalAndMergeSignalsIfMissing(signals any, opts ...MergeSignalsOption) error {
	if err := sse.MarshalAndMergeSignals(
		signals,
		append(opts, WithOnlyIfMissing(true))...,
	); err != nil {
		return fmt.Errorf("failed to merge signals if missing: %w", err)
	}
	return nil
}

func (sse *ServerSentEventGenerator) MergeSignalsIfMissingRaw(signalsJSON string) error {
	if err := sse.MergeSignals([]byte(signalsJSON), WithOnlyIfMissing(true)); err != nil {
		return fmt.Errorf("failed to merge signals if missing: %w", err)
	}
	return nil
}
```

## execute.go

```go
package datastar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ExecuteScriptOptions struct {
	EventID       string
	RetryDuration time.Duration
	Attributes    []string
	AutoRemove    *bool
}

type ExecuteScriptOption func(*ExecuteScriptOptions)

func WithExecuteScriptEventID(id string) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.EventID = id
	}
}

func WithExecuteScriptRetryDuration(retryDuration time.Duration) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.RetryDuration = retryDuration
	}
}

func WithExecuteScriptAttributes(attributes ...string) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.Attributes = attributes
	}
}

func WithExecuteScriptAttributeKVs(kvs ...string) ExecuteScriptOption {
	if len(kvs)%2 != 0 {
		panic("WithExecuteScriptAttributeKVs requires an even number of arguments")
	}
	attributes := make([]string, 0, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		attribute := fmt.Sprintf("%s %s", kvs[i], kvs[i+1])
		attributes = append(attributes, attribute)
	}
	return WithExecuteScriptAttributes(attributes...)
}

func WithExecuteScriptAutoRemove(autoremove bool) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.AutoRemove = &autoremove
	}
}

func (sse *ServerSentEventGenerator) ExecuteScript(scriptContents string, opts ...ExecuteScriptOption) error {
	options := &ExecuteScriptOptions{
		RetryDuration: DefaultSseRetryDuration,
		Attributes:    []string{"type module"},
	}
	for _, opt := range opts {
		opt(options)
	}

	sendOpts := make([]SSEEventOption, 0, 2)
	if options.EventID != "" {
		sendOpts = append(sendOpts, WithSSEEventId(options.EventID))
	}

	if options.RetryDuration != DefaultSseRetryDuration {
		sendOpts = append(sendOpts, WithSSERetryDuration(options.RetryDuration))
	}

	dataLines := make([]string, 0, 64)
	if options.AutoRemove != nil && *options.AutoRemove != DefaultExecuteScriptAutoRemove {
		dataLines = append(dataLines, AutoRemoveDatalineLiteral+strconv.FormatBool(*options.AutoRemove))
	}
	dataLinesJoined := strings.Join(dataLines, NewLine)

	if dataLinesJoined != DefaultExecuteScriptAttributes {
		for _, attribute := range options.Attributes {
			dataLines = append(dataLines, AttributesDatalineLiteral+attribute)
		}
	}

	scriptLines := strings.Split(scriptContents, NewLine)
	for _, line := range scriptLines {
		dataLines = append(dataLines, ScriptDatalineLiteral+line)
	}

	return sse.Send(
		EventTypeExecuteScript,
		dataLines,
		sendOpts...,
	)
}
```

## execute-script-sugar.go

```go
package datastar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ExecuteScriptOptions struct {
	EventID       string
	RetryDuration time.Duration
	Attributes    []string
	AutoRemove    *bool
}

type ExecuteScriptOption func(*ExecuteScriptOptions)

func WithExecuteScriptEventID(id string) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.EventID = id
	}
}

func WithExecuteScriptRetryDuration(retryDuration time.Duration) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.RetryDuration = retryDuration
	}
}

func WithExecuteScriptAttributes(attributes ...string) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.Attributes = attributes
	}
}

func WithExecuteScriptAttributeKVs(kvs ...string) ExecuteScriptOption {
	if len(kvs)%2 != 0 {
		panic("WithExecuteScriptAttributeKVs requires an even number of arguments")
	}
	attributes := make([]string, 0, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		attribute := fmt.Sprintf("%s %s", kvs[i], kvs[i+1])
		attributes = append(attributes, attribute)
	}
	return WithExecuteScriptAttributes(attributes...)
}

func WithExecuteScriptAutoRemove(autoremove bool) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.AutoRemove = &autoremove
	}
}

func (sse *ServerSentEventGenerator) ExecuteScript(scriptContents string, opts ...ExecuteScriptOption) error {
	options := &ExecuteScriptOptions{
		RetryDuration: DefaultSseRetryDuration,
		Attributes:    []string{"type module"},
	}
	for _, opt := range opts {
		opt(options)
	}

	sendOpts := make([]SSEEventOption, 0, 2)
	if options.EventID != "" {
		sendOpts = append(sendOpts, WithSSEEventId(options.EventID))
	}

	if options.RetryDuration != DefaultSseRetryDuration {
		sendOpts = append(sendOpts, WithSSERetryDuration(options.RetryDuration))
	}

	dataLines := make([]string, 0, 64)
	if options.AutoRemove != nil && *options.AutoRemove != DefaultExecuteScriptAutoRemove {
		dataLines = append(dataLines, AutoRemoveDatalineLiteral+strconv.FormatBool(*options.AutoRemove))
	}
	dataLinesJoined := strings.Join(dataLines, NewLine)

	if dataLinesJoined != DefaultExecuteScriptAttributes {
		for _, attribute := range options.Attributes {
			dataLines = append(dataLines, AttributesDatalineLiteral+attribute)
		}
	}

	scriptLines := strings.Split(scriptContents, NewLine)
	for _, line := range scriptLines {
		dataLines = append(dataLines, ScriptDatalineLiteral+line)
	}

	return sse.Send(
		EventTypeExecuteScript,
		dataLines,
		sendOpts...,
	)
}
```