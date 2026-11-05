//go:build integration

// Integration test against a real AI stack (inference + Qdrant):
//
//	go test -tags integration -run Integration ./...
//	AI_STACK_ENDPOINTS=/path/to/endpoints.env go test -tags integration ./...
//
// It uses a throwaway collection and deletes it afterwards.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIntegrationRealStack(t *testing.T) {
	cfg, err := loadConfig(envOr("AI_STACK_ENDPOINTS", "/etc/ai-stack/endpoints.env"))
	if err != nil {
		t.Fatal(err)
	}
	collection := collectionName(fmt.Sprintf("it-%d", time.Now().UnixNano()), cfg.EmbedSource, cfg.EmbedDims)
	store, err := newQdrantStore(cfg.QdrantGRPC, cfg.QdrantAPIKey, collection, cfg.EmbedDims)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		_ = store.client.DeleteCollection(context.Background(), collection)
		_ = store.Close()
	})

	cs := connect(t, NewNotes(newOpenAIEmbedder(cfg.InferenceBaseURL, cfg.EmbedModel, cfg.EmbedDims), store))
	for _, text := range []string{
		"The NAS backs up every night at 2am to the offsite bucket.",
		"Grandma's lasagna needs ricotta, mozzarella and fresh basil.",
		"The homelab router's admin page is at 192.168.1.1.",
	} {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "add_note", Arguments: map[string]any{"text": text}})
		if err != nil || res.IsError {
			t.Fatalf("add_note %q: err=%v result=%+v", text, err, res)
		}
	}

	// A real embedding model should match by meaning, not shared words.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_notes",
		Arguments: map[string]any{"query": "when do my files get copied for safekeeping?", "limit": 1},
	})
	if err != nil || res.IsError {
		t.Fatalf("search_notes: err=%v result=%+v", err, res)
	}
	var out SearchNotesOut
	raw, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(raw, &out)
	if len(out.Results) != 1 || out.Results[0].Text != "The NAS backs up every night at 2am to the offsite bucket." {
		t.Fatalf("best match = %+v, want the backup note", out.Results)
	}
	t.Logf("matched %q (score %.3f) in %s", out.Results[0].Text, out.Results[0].Score, collection)
}
