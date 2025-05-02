package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/a-h/templ"
	"github.com/nats-io/nats.go/jetstream"
	datastar "github.com/starfederation/datastar/sdk/go"
)

// RenderFuncB: minimal signature – render given message into the SSE stream.
type RenderFuncB func(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator) error

type renderer struct {
	pattern string
	match   func(string) bool
	fn      RenderFuncB
}

// newRenderer creates a renderer matching a specific subject pattern (with wildcards).
func newRenderer(pattern string, fn RenderFuncB) renderer {
	return renderer{
		pattern: pattern,
		match:   func(subj string) bool { return SubjectMatches(pattern, subj) },
		fn:      fn,
	}
}

// newTypedRenderer decodes the JSON payload into T and invokes handler.
func newTypedRenderer[T any](pattern string, handler func(context.Context, jetstream.Msg, *datastar.ServerSentEventGenerator, T) error) renderer {
	return newRenderer(pattern, func(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator) error {
		var p T
		dec := json.NewDecoder(bytes.NewReader(msg.Data()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("decode %T: %w", p, err)
		}
		return handler(ctx, msg, sse, p)
	})
}

// Helper for subscription message-box renderers that want a precomputed selector.
func newSubRenderer[T any](pattern string, handler func(context.Context, jetstream.Msg, *datastar.ServerSentEventGenerator, string, T) error) renderer {
	sel := SelectorFor(pattern) + "-msg"
	return newTypedRenderer[T](pattern, func(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, p T) error {
		return handler(ctx, msg, sse, sel, p)
	})
}

// fallback renderer renders any message as <pre> in corresponding msg box.
var fallback = newRenderer(
	">",
	func(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator) error {
		selector := SelectorFor(msg.Subject()) + "-msg"
		frag := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, _ = fmt.Fprintf(w, "<pre>%s\n%s</pre>", msg.Subject(), string(msg.Data()))
			return nil
		})
		return sse.MergeFragmentTempl(
			frag,
			datastar.WithSelector(selector),
			datastar.WithMergeAppend(),
		)
	},
)
