package platform

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"binrun/ui"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HTTPServerConfig holds HTTP server tunables.
type HTTPServerConfig struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	EnableTLS    bool   // whether to use HTTPS
	CertFile     string // path to TLS certificate
	KeyFile      string // path to TLS private key
}

// Global cookie store (for demo; in prod, use env/config for key)
var CookieStore = sessions.NewCookieStore([]byte("very-secret-key-change-me"))

// Session middleware to assign/load session ID and set in context
func SessionMiddleware(store *sessions.CookieStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, _ := store.Get(r, "binrun")
			id, ok := sess.Values["id"].(string)
			if !ok || id == "" {
				id = uuid.NewString()
				sess.Values["id"] = id
				sess.Options = &sessions.Options{
					Path:     "/",
					MaxAge:   60 * 60 * 24 * 7, // 1 week
					HttpOnly: true,
					Secure:   r.TLS != nil,
					SameSite: http.SameSiteLaxMode,
				}
				_ = sess.Save(r, w)
			}
			ctx := context.WithValue(r.Context(), sessionCtxKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RunHTTPServer starts an HTTP server and returns a channel that will receive
// an error when the server exits (gracefully or not).
func RunHTTPServer(ctx context.Context, nc *nats.Conn, cfg HTTPServerConfig) <-chan error {
	errCh := make(chan error, 1)

	r := chi.NewRouter()
	r.Use(SessionMiddleware(CookieStore))
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(chiLogger)
	r.Use(middleware.Recoverer)

	// metrics endpoint
	r.Method(http.MethodGet, "/metrics", promhttp.Handler())

	// JetStream context for handlers
	js, _ := jetstream.New(nc)

	// application routes
	r.Get("/health", Health)
	r.Post("/command/*", SendCommand(nc, js))

	// Terminal endpoint
	r.Post("/terminal", TerminalCommandHandler(js))

	// UI root route using Templ
	r.Get("/", templ.Handler(ui.Index()).ServeHTTP)

	// static assets
	staticFS, _ := fs.Sub(ui.StaticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	r.Handle("/favicon.svg", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(ui.FaviconSVG)
	}))

	r.Get("/ui", UIStream(js))
	r.Post("/session/load/{preset}", LoadPresetHandler(js))

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		// wait for context cancellation then shutdown
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			errCh <- err
			return
		}
		errCh <- ctx.Err()
	}()

	go func() {
		var err error
		if cfg.EnableTLS {
			err = srv.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	return errCh
}

// chiLogger is a lightweight slog adapter for chi middleware.
func chiLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t0 := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(t0)
		routePattern := chi.RouteContext(r.Context()).RoutePattern()
		status := http.StatusOK // we don't have after-the-fact status easily w/o wrapper
		HTTPRequestsTotal.WithLabelValues(r.Method, routePattern, fmt.Sprint(status)).Inc()
		HTTPDuration.WithLabelValues(r.Method, routePattern).Observe(duration.Seconds())
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "route", routePattern, "duration", duration)
	})
}

// SessionID returns the session ID from the request context.
type sessionCtxKey struct{}

func SessionID(r *http.Request) string {
	id, _ := r.Context().Value(sessionCtxKey{}).(string)
	return id
}
