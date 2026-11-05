package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Notes is the business logic: embed text, then store or search it.
// It knows nothing about MCP, so it can be reused by your harness directly.
type Notes struct {
	embedder Embedder
	store    Store

	mu      sync.Mutex
	ensured bool
	now     func() time.Time
}

func NewNotes(e Embedder, s Store) *Notes {
	return &Notes{embedder: e, store: s, now: time.Now}
}

// ready creates the collection the first time any tool runs, so the server
// can start even if Qdrant comes up a little later. A failure is retried on
// the next call.
func (n *Notes) ready(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ensured {
		return nil
	}
	if err := n.store.Ensure(ctx); err != nil {
		return fmt.Errorf("prepare collection: %w", err)
	}
	n.ensured = true
	return nil
}

// Add stores a note. The ID is derived from the text, so adding the same
// text twice updates one note instead of creating a duplicate.
func (n *Notes) Add(ctx context.Context, text string, tags []string) (Note, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Note{}, errors.New("text is empty")
	}
	if err := n.ready(ctx); err != nil {
		return Note{}, err
	}
	vec, err := n.embedder.Embed(ctx, text)
	if err != nil {
		return Note{}, err
	}
	note := Note{ID: idFor(text), Text: text, Tags: tags, CreatedAt: n.now().UTC().Format(time.RFC3339)}
	if err := n.store.Upsert(ctx, note, vec); err != nil {
		return Note{}, fmt.Errorf("store note: %w", err)
	}
	return note, nil
}

// Search returns the notes closest in meaning to query.
func (n *Notes) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is empty")
	}
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	if err := n.ready(ctx); err != nil {
		return nil, err
	}
	vec, err := n.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	return n.store.Search(ctx, vec, limit)
}

// idFor makes a stable UUID (Qdrant point IDs must be UUIDs or integers)
// from a SHA-256 of the text, formatted as a version-8 UUID.
func idFor(text string) string {
	h := sha256.Sum256([]byte(text))
	h[6] = (h[6] & 0x0f) | 0x80 // version 8 (custom)
	h[8] = (h[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
