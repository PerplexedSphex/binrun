package platform

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"binrun/internal/messages"
	components "binrun/ui/components"

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
			// Entry doesn't exist, create a default one with just terminal
			slog.Info("UIStream: No session KV found, creating default", "sid", sid)
			termSubj := messages.TerminalFreezeSubject(sid)
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

		// Parse layout if present
		layout, err := ParseLayout(info.Layout)
		if err != nil {
			slog.Warn("Invalid layout in session", "sid", sid, "err", err)
			// Continue with no layout
		}

		if len(info.Subscriptions) == 0 {
			http.Error(w, "no subscriptions", 404)
			return
		}

		// Render grid for initial subscriptions (using the now-complete list)
		{
			// Use all subscriptions directly
			gridSubs := info.Subscriptions

			// Use layout-aware rendering if layout is present
			if layout != nil {
				// Convert layout for UI components
				uiLayout, err := ConvertLayoutForUI(layout)
				if err != nil {
					slog.Warn("Failed to convert layout for UI", "err", err)
					// Fallback to grid
					grid := components.SubscriptionsGrid(gridSubs)
					_ = sse.MergeFragmentTempl(grid)
				} else {
					// Render layout trees for panels defined in layout (left / main / right)
					panelNames := []string{"left", "main", "right"}
					for _, pn := range panelNames {
						if uiLayout.Panels != nil {
							if _, ok := uiLayout.Panels[pn]; !ok {
								continue
							}
						}
						tree := components.LayoutTree(uiLayout, pn)
						_ = sse.MergeFragmentTempl(tree)
					}
				}
			} else {
				// Fallback to simple grid
				grid := components.SubscriptionsGrid(gridSubs)
				_ = sse.MergeFragmentTempl(grid)
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
		currentSubs := info.Subscriptions // Already includes terminal and is sorted
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

				var newInfo SessionInfo
				_ = json.Unmarshal(update.Value(), &newInfo)

				// Parse new layout
				newLayout, err := ParseLayout(newInfo.Layout)
				if err != nil {
					slog.Warn("Invalid layout in session update", "sid", sid, "err", err)
				}

				newSubs := newInfo.Subscriptions
				// No terminal sub logic here

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
					// Use all subscriptions directly
					gridSubs := newInfo.Subscriptions

					// Use layout-aware rendering if layout is present
					if newLayout != nil {
						// Convert layout for UI components
						uiLayout, err := ConvertLayoutForUI(newLayout)
						if err != nil {
							slog.Warn("Failed to convert layout for UI", "err", err)
							// Fallback to grid
							grid := components.SubscriptionsGrid(gridSubs)
							_ = sse.MergeFragmentTempl(grid)
						} else {
							// Render layout trees for panels defined in layout
							panelNames := []string{"left", "main", "right"}
							for _, pn := range panelNames {
								if uiLayout.Panels != nil {
									if _, ok := uiLayout.Panels[pn]; !ok {
										continue
									}
								}
								tree := components.LayoutTree(uiLayout, pn)
								_ = sse.MergeFragmentTempl(tree)
							}
						}

						// Update stored layout reference
						layout = newLayout

						// Recreate renderer set and consumer
						sessionRenderers = runtime.ForSubjects(newSubs)
						consumerCancel()
						<-consumerDone
						consumerCancel, consumerDone = createConsumer(newSubs, sessionRenderers)
						currentSubs = newSubs
					}
				}
			}
		}()

		<-ctx.Done() // Wait for disconnect
	}
}
