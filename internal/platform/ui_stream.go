package platform

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"binrun/internal/messages"
	components "binrun/ui/components"

	layoutpkg "binrun/internal/layout"
	runtime "binrun/internal/runtime"

	"github.com/nats-io/nats.go/jetstream"
	datastar "github.com/starfederation/datastar/sdk/go"
)

// UIStream is the SSE handler for /ui
func UIStream(js jetstream.JetStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sse := datastar.NewSSE(w, r)
		sid := SessionID(r)

		kv, _ := js.KeyValue(r.Context(), "sessions")
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// --- Get session subscriptions, ensuring terminal sub exists in KV ---
		entry, err := kv.Get(ctx, sid)
		if err != nil {
			// No session data: initialize default
			slog.Info("UIStream: No session KV found, creating default", "sid", sid)
			termSubj := messages.TerminalFreezeSubject(sid)
			dataObj := layoutpkg.SessionData{Subscriptions: []string{termSubj}}
			raw, _ := json.Marshal(dataObj)
			if _, putErr := kv.Put(ctx, sid, raw); putErr != nil {
				slog.Error("UIStream: Failed to put default session KV", "sid", sid, "err", putErr)
				http.Error(w, "internal error", 500)
				return
			}
			entry, err = kv.Get(ctx, sid)
			if err != nil {
				slog.Error("UIStream: Failed to re-fetch session KV after create", "sid", sid, "err", err)
				http.Error(w, "internal error", 500)
				return
			}
		}
		// Load unified session state
		sess, err := layoutpkg.LoadSessionData(entry.Value())
		if err != nil {
			http.Error(w, "invalid session info", 500)
			return
		}
		// Use typed state
		subs := sess.Subscriptions
		layoutTree := sess.Layout

		if layoutTree != nil && layoutTree.Validate() != nil {
			slog.Warn("Invalid layout in session", "sid", sid)
		}

		if len(subs) == 0 {
			http.Error(w, "no subscriptions", 404)
			return
		}

		// Compute and render layout fragments
		frags := layoutpkg.LayoutFragments(layoutTree, subs)
		for _, frag := range frags {
			_ = sse.MergeFragmentTempl(frag.Component,
				datastar.WithSelectorID(frag.SelectorID),
			)
		}
		// Clear existing command forms and render new ones
		_ = sse.MergeFragments("",
			datastar.WithSelectorID("left-panel-content"),
			datastar.WithMergeMode(datastar.FragmentMergeModeInner),
		)
		if len(sess.Commands) > 0 {
			for _, cd := range sess.Commands {
				frag := components.CommandForm(cd.Type, cd.Script, cd.Defaults)
				_ = sse.MergeFragmentTempl(frag,
					datastar.WithSelectorID("left-panel-content"),
					datastar.WithMergeAppend(),
				)
			}
		}

		// --- Setup for watcher ---
		consumerCancel := func() {}
		consumerDone := make(chan struct{})

		createConsumer := func(subs []string, renderers []runtime.Renderer) (context.CancelFunc, chan struct{}) {
			cctx, ccancel := context.WithCancel(ctx)
			cdone := make(chan struct{})

			cons, err := js.CreateConsumer(ctx, "EVENT", jetstream.ConsumerConfig{
				AckPolicy:      jetstream.AckNonePolicy,
				FilterSubjects: subs,
				DeliverPolicy:  jetstream.DeliverAllPolicy, // Crucial for replay
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
					for _, r := range renderers {
						if r.MatchFunc(msg.Subject()) {
							if err := r.RenderFunc(ctx, msg, sse); err != nil {
								slog.Warn("render", "subj", msg.Subject(), "err", err)
							}
							break
						}
					}
				})
				if err != nil {
					slog.Warn("consume failed", "err", err)
				}
				<-cctx.Done()
			}()
			return ccancel, cdone
		}

		// --- Start initial consumer ---
		currentSubs := subs // Already includes terminal and is sorted
		sessionRenderers := runtime.ForSubjects(currentSubs)
		consumerCancel, consumerDone = createConsumer(currentSubs, sessionRenderers)

		// --- Watch for live updates ---
		watcher, err := kv.Watch(ctx, sid)
		if err != nil {
			http.Error(w, "failed to watch session", 500)
			return
		}
		defer watcher.Stop()

		go func() {
			for update := range watcher.Updates() {
				if update == nil {
					continue
				}
				if update.Operation() == jetstream.KeyValueDelete {
					cancel()
					return
				}

				// Load updated session state
				newState, err := layoutpkg.LoadSessionData(update.Value())
				if err != nil {
					slog.Warn("Invalid session update", "sid", sid, "err", err)
					continue
				}
				newSubs := newState.Subscriptions
				newLayout := newState.Layout

				// Compare sorted lists
				subsChanged := len(newSubs) != len(currentSubs)
				if !subsChanged {
					for i := range newSubs {
						if newSubs[i] != currentSubs[i] {
							subsChanged = true
							break
						}
					}
				}

				if subsChanged {
					// Compute and render layout fragments
					frags := layoutpkg.LayoutFragments(newLayout, newSubs)
					for _, frag := range frags {
						_ = sse.MergeFragmentTempl(frag.Component,
							datastar.WithSelectorID(frag.SelectorID),
						)
					}
					// Clear existing command forms before injecting new ones
					_ = sse.MergeFragments("",
						datastar.WithSelectorID("left-panel-content"),
						datastar.WithMergeMode(datastar.FragmentMergeModeInner),
					)
					// Render command forms in left sidebar
					if len(newState.Commands) > 0 {
						for _, cd := range newState.Commands {
							frag := components.CommandForm(cd.Type, cd.Script, cd.Defaults)
							_ = sse.MergeFragmentTempl(frag,
								datastar.WithSelectorID("left-panel-content"),
								datastar.WithMergeAppend(),
							)
						}
					}

					// Update stored layout reference
					layoutTree = newLayout

					// Recreate renderer set and consumer
					sessionRenderers = runtime.ForSubjects(newSubs)
					consumerCancel()
					<-consumerDone
					consumerCancel, consumerDone = createConsumer(newSubs, sessionRenderers)
					currentSubs = newSubs
				}
			}
		}()

		<-ctx.Done() // Wait for disconnect
	}
}
