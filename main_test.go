package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()

	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := &server{store: store, baseURL: "https://shorty.com"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/condense", srv.handleCondense)
	mux.HandleFunc("GET /{slug}", srv.handleRedirect)
	return mux
}

func condense(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/condense", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCondenseAndRedirect(t *testing.T) {
	h := newTestHandler(t)

	rec := condense(t, h, `{"url":"https://example.com/long"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}

	var out condenseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(out.URL, "https://shorty.com/") {
		t.Fatalf("url = %q, want https://shorty.com/<slug>", out.URL)
	}

	slug := strings.TrimPrefix(out.URL, "https://shorty.com/")
	if len(slug) != slugLength {
		t.Fatalf("slug %q has length %d, want %d", slug, len(slug), slugLength)
	}

	redirect := httptest.NewRecorder()
	h.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/"+slug, nil))

	if redirect.Code != http.StatusFound {
		t.Fatalf("redirect status = %d, want 302", redirect.Code)
	}
	if got := redirect.Header().Get("Location"); got != "https://example.com/long" {
		t.Fatalf("Location = %q, want the original URL", got)
	}
}

func TestCondenseIsIdempotent(t *testing.T) {
	h := newTestHandler(t)

	first := condense(t, h, `{"url":"https://example.com/same"}`)
	second := condense(t, h, `{"url":"https://example.com/same"}`)

	if first.Body.String() != second.Body.String() {
		t.Fatalf("same url returned different links:\n %s\n %s", first.Body, second.Body)
	}
}

func TestCondenseRejectsBadInput(t *testing.T) {
	h := newTestHandler(t)

	cases := []string{
		``,
		`not json`,
		`{}`,
		`{"url":"   "}`,
		`{"url":"ftp://example.com/file"}`,
		`{"url":"mailto:someone@example.com"}`,
		`{"url":"javascript:alert(1)"}`,
	}
	for _, body := range cases {
		rec := condense(t, h, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestUnknownSlugReturns404(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/zzzzz", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"https://example.com/a", "https://example.com/a", false},
		{"  https://example.com/a  ", "https://example.com/a", false},
		{"example.com/a", "https://example.com/a", false},
		{"http://example.com", "http://example.com", false},
		{"ftp://example.com", "", true},
		{"", "", true},
		{"https://", "", true},
	}
	for _, c := range cases {
		got, err := normalizeURL(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeURL(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeURL(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
