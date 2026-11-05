package main

import (
	"context"
	"errors"
	"regexp"
	"testing"
)

func TestAddAndSearch(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	notes := NewNotes(fakeEmbedder{dims: 64}, store)

	for _, text := range []string{
		"the router admin password is in bitwarden",
		"buy milk and eggs",
		"qdrant runs on port 6333 on bones",
	} {
		if _, err := notes.Add(ctx, text, nil); err != nil {
			t.Fatalf("Add(%q): %v", text, err)
		}
	}

	hits, err := notes.Search(ctx, "which port does qdrant use", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].Text != "qdrant runs on port 6333 on bones" {
		t.Errorf("best hit = %q, want the qdrant note", hits[0].Text)
	}
	if store.ensured != 1 {
		t.Errorf("Ensure called %d times, want once", store.ensured)
	}
}

func TestAddSameTextIsOneNote(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	notes := NewNotes(fakeEmbedder{dims: 16}, store)

	a, _ := notes.Add(ctx, "same note", []string{"x"})
	b, _ := notes.Add(ctx, "  same note  ", []string{"y"})
	if a.ID != b.ID {
		t.Fatalf("IDs differ: %s vs %s", a.ID, b.ID)
	}
	if len(store.notes) != 1 || store.notes[a.ID].Tags[0] != "y" {
		t.Fatalf("want one note with the latest tags, got %+v", store.notes)
	}
}

func TestValidation(t *testing.T) {
	notes := NewNotes(fakeEmbedder{dims: 8}, newMemStore())
	if _, err := notes.Add(context.Background(), "   ", nil); err == nil {
		t.Error("Add with empty text: want error")
	}
	if _, err := notes.Search(context.Background(), "", 5); err == nil {
		t.Error("Search with empty query: want error")
	}
}

func TestEnsureFailureIsRetried(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	store.ensureErr = errDown
	notes := NewNotes(fakeEmbedder{dims: 8}, store)

	if _, err := notes.Add(ctx, "first", nil); !errors.Is(err, errDown) {
		t.Fatalf("want errDown, got %v", err)
	}
	store.ensureErr = nil
	if _, err := notes.Add(ctx, "second", nil); err != nil {
		t.Fatalf("after Qdrant recovers: %v", err)
	}
}

func TestIDIsUUID(t *testing.T) {
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for _, s := range []string{"a", "hello world", "日本語"} {
		if id := idFor(s); !uuid.MatchString(id) {
			t.Errorf("idFor(%q) = %s, not a v8 UUID", s, id)
		}
	}
}
