package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	components "binrun/ui/components"

	"slices"

	"binrun/internal/runtime"

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
			// Entry doesn't exist, create a default one with just terminal
			slog.Info("UIStream: No session KV found, creating default", "sid", sid)
			termSubj := fmt.Sprintf("event.terminal.session.%s.freeze", sid)
			info := SessionInfo{Subscriptions: []string{termSubj}}
			data, _ := json.Marshal(info)
			if _, putErr := kv.Put(ctx, sid, data); putErr != nil {
				slog.Error("UIStream: Failed to put default session KV", "sid", sid, "err", putErr)
				http.Error(w, "internal error", 500)
				return
			}
			// Use this default info to proceed
			entry, err = kv.Get(ctx, sid) // Re-fetch to get the entry object
			if err != nil {
				// Should not happen after successful Put, but handle defensively
				slog.Error("UIStream: Failed to re-fetch session KV after create", "sid", sid, "err", err)
				http.Error(w, "internal error", 500)
				return
			}
		}

		var info SessionInfo
		if err := json.Unmarshal(entry.Value(), &info); err != nil {
			http.Error(w, "invalid session info", 500)
			return
		}

		termSubj := fmt.Sprintf("event.terminal.session.%s.freeze", sid)
		if !slices.Contains(info.Subscriptions, termSubj) {
			info.Subscriptions = append(info.Subscriptions, termSubj)
			slices.Sort(info.Subscriptions)
			data, _ := json.Marshal(info)
			// Update the KV store with the corrected list
			if _, putErr := kv.Put(ctx, sid, data); putErr != nil {
				slog.Error("UIStream: Failed to update session KV with terminal sub", "sid", sid, "err", putErr)
				// Don't fail the request, just log it. Proceed with the in-memory list.
			}
		}

		if len(info.Subscriptions) == 0 {
			http.Error(w, "no subscriptions", 404)
			return
		}

		// Render grid for initial subscriptions (using the now-complete list)
		{
			// Filter out terminal subjects before rendering the grid
			gridSubs := []string{}
			for _, s := range info.Subscriptions {
				if !strings.HasPrefix(s, "event.terminal.session.") {
					gridSubs = append(gridSubs, s)
				}
			}
			grid := components.SubscriptionsGrid(gridSubs)
			_ = sse.MergeFragmentTempl(grid)
		}

		// --- Setup for watcher ---
		consumerCancel := func() {}
		consumerDone := make(chan struct{})

		createConsumer := func(subs []string) (context.CancelFunc, chan struct{}) {
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
					// New unified dispatch via Renderers registry
					for _, r := range runtime.Renderers {
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
		currentSubs := info.Subscriptions // Already includes terminal and is sorted
		consumerCancel, consumerDone = createConsumer(currentSubs)

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

				var newInfo SessionInfo
				_ = json.Unmarshal(update.Value(), &newInfo)
				newSubs := newInfo.Subscriptions
				// Ensure terminal sub is present for comparison
				if !slices.Contains(newSubs, termSubj) {
					newSubs = append(newSubs, termSubj)
				}
				slices.Sort(newSubs)

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
					// Render grid update using actual KV subs (filter out terminal)
					gridSubs := []string{}
					for _, s := range newInfo.Subscriptions {
						if !strings.HasPrefix(s, "event.terminal.session.") {
							gridSubs = append(gridSubs, s)
						}
					}
					grid := components.SubscriptionsGrid(gridSubs)
					_ = sse.MergeFragmentTempl(grid)

					// Recreate consumer with new (sorted, terminal-inclusive) list
					consumerCancel()
					<-consumerDone
					consumerCancel, consumerDone = createConsumer(newSubs)
					currentSubs = newSubs
				}
			}
		}()

		<-ctx.Done() // Wait for disconnect
	}
}
