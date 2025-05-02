package core

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	components "binrun/ui/components"

	"github.com/a-h/templ"
	"github.com/nats-io/nats.go/jetstream"
	datastar "github.com/starfederation/datastar/sdk/go"
)

// RenderFunc renders a templ fragment for a given JetStream message
// Returns nil if the subject is not handled.
type RenderFunc func(msg jetstream.Msg) (templ.Component, error)

// Registry of subject prefix to render function
var renderers = []struct {
	Prefix string
	Fn     RenderFunc
}{
	{"event.orders.", renderOrderFrag},
	{"event.chat.", renderChatFrag},
	{"event.presence.", renderPresenceFrag},
}

// UIStream is the SSE handler for /ui
func UIStream(js jetstream.JetStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sse := datastar.NewSSE(w, r)
		sid := SessionID(r)

		kv, _ := js.KeyValue(r.Context(), "sessions")
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// --- Fast-path: create initial consumer immediately ---
		entry, err := kv.Get(ctx, sid)
		if err != nil {
			http.Error(w, "no session subscriptions", 404)
			return
		}
		var info SessionInfo
		if err := json.Unmarshal(entry.Value(), &info); err != nil {
			http.Error(w, "invalid session info", 500)
			return
		}
		if len(info.Subscriptions) == 0 {
			http.Error(w, "no subscriptions", 404)
			return
		}

		// Render grid for initial subscriptions
		{
			grid := components.SubscriptionsGrid(info.Subscriptions)
			_ = sse.MergeFragmentTempl(grid)
		}

		// Used to cancel the previous consumer
		consumerCancel := func() {}
		consumerDone := make(chan struct{})

		// Helper to test NATS wildcard match
		subjectMatches := func(pattern, subj string) bool {
			if pattern == subj {
				return true
			}
			pTok := strings.Split(pattern, ".")
			sTok := strings.Split(subj, ".")
			for i, pt := range pTok {
				if pt == ">" {
					return true // matches remainder
				}
				if i >= len(sTok) {
					return false
				}
				if pt == "*" {
					continue
				}
				if pt != sTok[i] {
					return false
				}
			}
			return len(sTok) == len(pTok)
		}

		// Helper to create a consumer and handler
		createConsumer := func(subs []string) (context.CancelFunc, chan struct{}) {
			cctx, ccancel := context.WithCancel(ctx)
			cdone := make(chan struct{})

			// copy subs to avoid data races
			patterns := make([]string, len(subs))
			copy(patterns, subs)

			cons, err := js.CreateConsumer(ctx, "EVENT", jetstream.ConsumerConfig{
				AckPolicy:      jetstream.AckNonePolicy,
				FilterSubjects: subs,
			})
			if err != nil {
				slog.Warn("failed to create consumer", "err", err)
				ccancel()
				close(cdone)
				return ccancel, cdone
			}
			go func() {
				defer close(cdone)
				_, err := cons.Consume(func(msg jetstream.Msg) {
					frag := dispatchToRenderer(msg)
					if frag == nil {
						return
					}
					for _, pat := range patterns {
						if subjectMatches(pat, msg.Subject()) {
							target := subjToID(pat) + "-msg"
							if err := sse.MergeFragmentTempl(frag, datastar.WithSelectorID(target), datastar.WithMergeAppend()); err != nil {
								slog.Warn("Failed to send fragment", "err", err)
							}
						}
					}
				})
				if err != nil {
					slog.Warn("failed to consume", "err", err)
				}
				<-cctx.Done()
			}()
			return ccancel, cdone
		}

		// Start initial consumer
		consumerCancel, consumerDone = createConsumer(info.Subscriptions)

		// --- Watch for live updates ---
		watcher, err := kv.Watch(ctx, sid)
		if err != nil {
			http.Error(w, "failed to watch session", 500)
			return
		}

		go func() {
			currentSubs := info.Subscriptions
			for update := range watcher.Updates() {
				// Ignore nil sentinel (initial replay complete)
				if update == nil {
					continue
				}
				if update.Operation() == jetstream.KeyValueDelete {
					cancel() // closes the SSE stream
					return
				}
				var newInfo SessionInfo
				if err := json.Unmarshal(update.Value(), &newInfo); err != nil {
					slog.Warn("invalid session info", "err", err)
					cancel()
					return
				}
				if len(newInfo.Subscriptions) == 0 {
					cancel()
					return
				}
				// Only rebuild if subscriptions changed
				subsChanged := len(newInfo.Subscriptions) != len(currentSubs)
				if !subsChanged {
					for i, s := range newInfo.Subscriptions {
						if s != currentSubs[i] {
							subsChanged = true
							break
						}
					}
				}
				if subsChanged {
					// send new grid morph
					grid := components.SubscriptionsGrid(newInfo.Subscriptions)
					_ = sse.MergeFragmentTempl(grid)

					// recreate consumer
					consumerCancel()
					<-consumerDone
					consumerCancel, consumerDone = createConsumer(newInfo.Subscriptions)
					currentSubs = newInfo.Subscriptions
				}
			}
		}()

		<-ctx.Done() // Wait for disconnect
	}
}

// dispatchToRenderer finds the first matching renderer for the subject
func dispatchToRenderer(msg jetstream.Msg) templ.Component {
	subj := msg.Subject()
	for _, r := range renderers {
		if strings.HasPrefix(subj, r.Prefix) {
			frag, err := r.Fn(msg)
			if err != nil {
				slog.Warn("renderer error", "subject", subj, "err", err)
				return genericFallbackFrag(msg)
			}
			return frag
		}
	}
	return genericFallbackFrag(msg)
}

// Example renderer for orders
func renderOrderFrag(msg jetstream.Msg) (templ.Component, error) {
	// TODO: Unmarshal msg.Data() and return a templ fragment
	return nil, nil
}
func renderChatFrag(msg jetstream.Msg) (templ.Component, error) {
	return nil, nil
}
func renderPresenceFrag(msg jetstream.Msg) (templ.Component, error) {
	return nil, nil
}

// Fallback: render as <pre> with subject and data
func genericFallbackFrag(msg jetstream.Msg) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("<pre>" + msg.Subject() + "\n" + string(msg.Data()) + "</pre>"))
		return nil
	})
}

func subjToID(subj string) string {
	s := strings.ReplaceAll(subj, ".", "-")
	s = strings.ReplaceAll(s, ">", "fullwild")
	s = strings.ReplaceAll(s, "*", "wild")
	return "sub-" + s
}
