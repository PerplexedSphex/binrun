package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	components "binrun/ui/components"

	"slices"

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
	// terminal events will be handled by generic fallback for now
}

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
			termSubj := fmt.Sprintf("terminal.session.%s.event", sid)
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

		termSubj := fmt.Sprintf("terminal.session.%s.event", sid)
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
				if !strings.HasPrefix(s, "terminal.session.") {
					gridSubs = append(gridSubs, s)
				}
			}
			grid := components.SubscriptionsGrid(gridSubs)
			_ = sse.MergeFragmentTempl(grid)
		}

		// --- Setup for watcher ---
		consumerCancel := func() {}
		consumerDone := make(chan struct{})

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

		createConsumer := func(subs []string) (context.CancelFunc, chan struct{}) {
			cctx, ccancel := context.WithCancel(ctx)
			cdone := make(chan struct{})
			patterns := make([]string, len(subs))
			copy(patterns, subs)

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
					// --- Terminal event special-case ---
					if strings.HasPrefix(msg.Subject(), "terminal.session.") {
						var evt struct {
							LineID string `json:"line_id"`
							Cmd    string `json:"cmd"`
							Output string `json:"output"`
						}
						_ = json.Unmarshal(msg.Data(), &evt)
						frozen := components.TerminalFrozenLine(evt.Cmd, evt.Output)
						if err := sse.MergeFragmentTempl(frozen, datastar.WithSelectorID("term-"+evt.LineID)); err != nil {
							slog.Warn("freeze fail", "err", err)
						}
						nextNum := 1
						if len(evt.LineID) > 1 {
							fmt.Sscanf(evt.LineID[1:], "%d", &nextNum)
							nextNum++
						}
						nextID := fmt.Sprintf("L%d", nextNum)
						promptFrag := components.TerminalPrompt(nextID)
						if err := sse.MergeFragmentTempl(promptFrag, datastar.WithSelectorID("terminal-lines"), datastar.WithMergeAppend()); err != nil {
							slog.Warn("prompt fail", "err", err)
						}
						return
					}

					// --- Generic path ---
					frag := dispatchToRenderer(msg)
					if frag == nil {
						return
					}
					subj := msg.Subject()
					for _, pat := range patterns {
						if subjectMatches(pat, subj) {
							target := subjToID(pat) + "-msg"
							if err := sse.MergeFragmentTempl(frag, datastar.WithSelectorID(target), datastar.WithMergeAppend()); err != nil {
								slog.Warn("send fail", "err", err)
							}
							break // Important: Render only once per message
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
						if !strings.HasPrefix(s, "terminal.session.") {
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
