package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"sort"
	"sync"
)

// fakeEmbedder makes a deterministic vector from the words of the text, so
// texts that share words are "close". Good enough to test search ordering.
type fakeEmbedder struct{ dims int }

func (f fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, f.dims)
	word := []byte{}
	flush := func() {
		if len(word) == 0 {
			return
		}
		h := sha256.Sum256(word)
		vec[int(h[0])%f.dims]++
		word = word[:0]
	}
	for _, c := range []byte(text) {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			word = append(word, c)
		} else {
			flush()
		}
	}
	flush()
	return vec, nil
}

// memStore is an in-memory Store with cosine similarity.
type memStore struct {
	mu        sync.Mutex
	notes     map[string]Note
	vecs      map[string][]float32
	ensureErr error
	ensured   int
}

func newMemStore() *memStore {
	return &memStore{notes: map[string]Note{}, vecs: map[string][]float32{}}
}

func (m *memStore) Ensure(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ensureErr != nil {
		return m.ensureErr
	}
	m.ensured++
	return nil
}

func (m *memStore) Upsert(_ context.Context, n Note, vec []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes[n.ID], m.vecs[n.ID] = n, vec
	return nil
}

func (m *memStore) Search(_ context.Context, vec []float32, limit int) ([]Hit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var hits []Hit
	for id, n := range m.notes {
		hits = append(hits, Hit{Note: n, Score: cosine(vec, m.vecs[id])})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func cosine(a, b []float32) float32 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i] * b[i])
		na += float64(a[i] * a[i])
		nb += float64(b[i] * b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

var errDown = errors.New("qdrant down")
