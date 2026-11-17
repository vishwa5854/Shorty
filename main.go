// Command shorty is a tiny URL shortener.
//
//	POST /api/condense  {"url":"https://example.com"}  ->  {"url":"https://shorty.com/abc1234"}
//	GET  /{slug}        302 redirect to the stored URL
//
// Static UI lives in web/ and is embedded into the binary.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFiles embed.FS

type server struct {
	store   *Store
	baseURL string // optional public base URL; empty means derive it from the request
}

func main() {
	addr := flag.String("addr", envOr("SHORTY_ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("SHORTY_DB", "shorty.db"), "sqlite database file")
	baseURL := flag.String("base-url", envOr("SHORTY_BASE_URL", ""), "public base URL, e.g. https://shorty.com (empty: derive from the request)")
	flag.Parse()

	store, err := OpenStore(*dbPath)
	if err != nil {
		log.Fatalf("open database %q: %v", *dbPath, err)
	}
	defer store.Close()

	static, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("static files: %v", err)
	}

	srv := &server{store: store, baseURL: strings.TrimRight(*baseURL, "/")}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/condense", srv.handleCondense)
	mux.HandleFunc("GET /api/health", srv.handleHealth)
	mux.HandleFunc("GET /{slug}", srv.handleRedirect)

	// Exact match only, so an unknown path does not get the UI page and a
	// wrong-method call to an API route returns 405 instead of a 404 page.
	fileServer := http.FileServer(http.FS(static))
	mux.Handle("GET /{$}", fileServer)
	mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("shorty listening on %s (db %s)", *addr, *dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// --- handlers ---------------------------------------------------------------

type condenseRequest struct {
	URL string `json:"url"`
}

type condenseResponse struct {
	URL string `json:"url"`
}

func (s *server) handleCondense(w http.ResponseWriter, r *http.Request) {
	var req condenseRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `body must be JSON like {"url":"https://example.com"}`)
		return
	}

	target, err := normalizeURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	slug, err := s.store.CreateOrGet(target)
	if err != nil {
		log.Printf("condense %q: %v", target, err)
		writeError(w, http.StatusInternalServerError, "could not save link")
		return
	}

	writeJSON(w, http.StatusOK, condenseResponse{URL: s.baseURLFor(r) + "/" + slug})
}

func (s *server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		http.NotFound(w, r)
		return
	}

	target, err := s.store.Lookup(slug)
	if errors.Is(err, ErrLinkNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("lookup %q: %v", slug, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.store.Hit(slug)

	// 302, not 301: a browser that caches a 301 would keep the old target even
	// if the row changes later, and this also keeps hit counts honest.
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "ok")
}

// --- helpers ----------------------------------------------------------------

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return "", errors.New("url is required")
	case len(raw) > 2048:
		return "", errors.New("url is too long (max 2048 characters)")
	}

	// Parse first: if a scheme is already present it must be http(s). Only a
	// bare "example.com/x" gets https:// prepended, so mailto:, ftp:, data: and
	// friends are rejected instead of being turned into https://mailto:...
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("url is not valid")
	}
	if u.Scheme == "" {
		raw = "https://" + raw
		if u, err = url.Parse(raw); err != nil {
			return "", errors.New("url is not valid")
		}
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("only http and https urls are allowed")
	}
	if u.Host == "" {
		return "", errors.New("url has no host")
	}
	return u.String(), nil
}

// baseURLFor returns the public origin used to build short links. Behind a
// reverse proxy the request's own Host/scheme are the internal ones, so the
// forwarded headers win when present.
func (s *server) baseURLFor(r *http.Request) string {
	if s.baseURL != "" {
		return s.baseURL
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	}

	host := r.Host
	if fwd := firstHeaderValue(r.Header.Get("X-Forwarded-Host")); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}

func firstHeaderValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func validSlug(slug string) bool {
	if slug == "" || len(slug) > 32 {
		return false
	}
	for _, c := range slug {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
