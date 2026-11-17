package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo
)

// ErrLinkNotFound is returned by Lookup when no row matches the slug.
var ErrLinkNotFound = errors.New("link not found")

const (
	slugAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	slugLength   = 5 // 62^5 ≈ 916e6 combinations, plenty for a homelab
)

const schema = `
CREATE TABLE IF NOT EXISTS links (
	slug       TEXT PRIMARY KEY,
	url        TEXT NOT NULL,
	hits       INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS links_url ON links(url);
`

// Store is the sqlite-backed link table.
type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// SQLite has one writer at a time; a single connection keeps writes
	// serialised in-process instead of failing with SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// CreateOrGet returns the slug for target, reusing an existing row when the
// same URL has been shortened before.
func (s *Store) CreateOrGet(target string) (string, error) {
	var slug string
	err := s.db.QueryRow(`SELECT slug FROM links WHERE url = ?`, target).Scan(&slug)
	switch {
	case err == nil:
		return slug, nil
	case !errors.Is(err, sql.ErrNoRows):
		return "", err
	}

	for attempt := 0; attempt < 5; attempt++ {
		slug, err = newSlug()
		if err != nil {
			return "", err
		}

		_, err = s.db.Exec(
			`INSERT INTO links (slug, url, created_at) VALUES (?, ?, ?)`,
			slug, target, time.Now().Unix(),
		)
		if err == nil {
			return slug, nil
		}
		if !isUniqueViolation(err) {
			return "", err
		}

		// Either the random slug collided or a concurrent request inserted this
		// URL first. If the URL is already there, reuse it; otherwise retry.
		var existing string
		if lookupErr := s.db.QueryRow(`SELECT slug FROM links WHERE url = ?`, target).Scan(&existing); lookupErr == nil {
			return existing, nil
		}
	}
	return "", errors.New("could not allocate a unique slug")
}

// Lookup resolves a slug to its target URL.
func (s *Store) Lookup(slug string) (string, error) {
	var target string
	err := s.db.QueryRow(`SELECT url FROM links WHERE slug = ?`, slug).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrLinkNotFound
	}
	return target, err
}

// Hit records one redirect. A failed count is not worth failing the redirect.
func (s *Store) Hit(slug string) {
	if _, err := s.db.Exec(`UPDATE links SET hits = hits + 1 WHERE slug = ?`, slug); err != nil {
		log.Printf("count hit for %q: %v", slug, err)
	}
}

func newSlug() (string, error) {
	buf := make([]byte, slugLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random slug: %w", err)
	}
	out := make([]byte, slugLength)
	for i, b := range buf {
		out[i] = slugAlphabet[int(b)%len(slugAlphabet)]
	}
	return string(out), nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
