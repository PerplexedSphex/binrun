package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"binrun/util"

	"github.com/a-h/templ"
	"github.com/nats-io/nats.go/jetstream"
	datastar "github.com/starfederation/datastar/sdk/go"
)

// RenderFuncB: minimal signature – render given message into the SSE stream.
type RenderFuncB func(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator) error

type Renderer struct {
	Pattern    string
	MatchFunc  func(string) bool
	RenderFunc RenderFuncB
}

// RendererSpec is a catalogue entry: a wildcard pattern and a factory that
// can build a concrete Renderer for a given subscription subject that matches
// the pattern.
type RendererSpec struct {
	Pattern string
	Build   func(subj string) Renderer
}

// Specs is filled by renderers.go during init and treated as read-only.
var Specs []RendererSpec

// ForSubjects returns a slice of Renderers suited for the exact subjects the
// UI stream is subscribing to. It materialises a renderer for every
// (subject, spec) pair where the subject matches the spec's wildcard pattern.
// The fallback renderer is always appended as last element.
func ForSubjects(subjects []string) []Renderer {
	out := make([]Renderer, 0)
	seen := make(map[string]struct{})
	for _, s := range subjects {
		for _, spec := range Specs {
			if util.SubjectMatches(spec.Pattern, s) {
				key := spec.Pattern + "|" + s
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, spec.Build(s))
			}
		}
	}
	// Ensure fallback renderer is last
	out = append(out, fallback)
	return out
}

// newRenderer creates a renderer matching a specific subject pattern (with wildcards).
func newRenderer(pattern string, fn RenderFuncB) Renderer {
	return Renderer{
		Pattern:    pattern,
		MatchFunc:  func(subj string) bool { return util.SubjectMatches(pattern, subj) },
		RenderFunc: fn,
	}
}

// newTypedRenderer decodes the JSON payload into T and invokes handler.
func newTypedRenderer[T any](pattern string, handler func(context.Context, jetstream.Msg, *datastar.ServerSentEventGenerator, T) error) Renderer {
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
func newSubRenderer[T any](pattern string, handler func(context.Context, jetstream.Msg, *datastar.ServerSentEventGenerator, string, T) error) Renderer {
	sel := util.SelectorFor(pattern) + "-msg"
	return newTypedRenderer[T](pattern, func(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator, p T) error {
		return handler(ctx, msg, sse, sel, p)
	})
}

// fallback renderer renders any message as <pre> in corresponding msg box.
var fallback = newRenderer(
	">",
	func(ctx context.Context, msg jetstream.Msg, sse *datastar.ServerSentEventGenerator) error {
		selector := util.SelectorFor(msg.Subject()) + "-msg"
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
