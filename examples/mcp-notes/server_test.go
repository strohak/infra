package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect starts the MCP server over in-memory transports and returns a
// client session: the same protocol a real client speaks, minus HTTP.
func connect(t *testing.T, notes *Notes) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := newMCPServer(notes).Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestToolsOverMCP(t *testing.T) {
	ctx := context.Background()
	cs := connect(t, NewNotes(fakeEmbedder{dims: 32}, newMemStore()))

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
	}
	if !names["add_note"] || !names["search_notes"] {
		t.Fatalf("tools = %v, want add_note and search_notes", names)
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "add_note",
		Arguments: map[string]any{"text": "the NAS backs up nightly at 2am", "tags": []string{"homelab"}},
	})
	if err != nil || res.IsError {
		t.Fatalf("add_note: err=%v result=%+v", err, res)
	}

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_notes",
		Arguments: map[string]any{"query": "when does the NAS back up", "limit": 3},
	})
	if err != nil || res.IsError {
		t.Fatalf("search_notes: err=%v result=%+v", err, res)
	}
	var out SearchNotesOut
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 || out.Results[0].Tags[0] != "homelab" {
		t.Fatalf("results = %+v", out.Results)
	}
}

func TestToolErrorReachesModel(t *testing.T) {
	cs := connect(t, NewNotes(fakeEmbedder{dims: 8}, newMemStore()))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "add_note", Arguments: map[string]any{"text": ""},
	})
	if err != nil {
		t.Fatalf("protocol error: %v (want a tool error the model can read)", err)
	}
	if !res.IsError {
		t.Fatal("want IsError for empty text")
	}
}

func TestBearerAuthOverHTTP(t *testing.T) {
	notes := NewNotes(fakeEmbedder{dims: 8}, newMemStore())
	ts := httptest.NewServer(newHTTPHandler(newMCPServer(notes), "s3cret"))
	defer ts.Close()

	ctx := context.Background()
	dial := func(token string) error {
		hc := &http.Client{Transport: headerRT{token: token}}
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
		cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", HTTPClient: hc, MaxRetries: -1}, nil)
		if err == nil {
			_, err = cs.ListTools(ctx, nil)
			_ = cs.Close()
		}
		return err
	}
	if err := dial("wrong"); err == nil {
		t.Error("wrong token: want error")
	}
	if err := dial("s3cret"); err != nil {
		t.Errorf("right token: %v", err)
	}

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("healthz: %v %v", err, resp)
	}
}

type headerRT struct{ token string }

func (h headerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+h.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestCheckExposure(t *testing.T) {
	cases := []struct {
		addr, token string
		ok          bool
	}{
		{"127.0.0.1:8765", "", true},
		{"localhost:8765", "", true},
		{"[::1]:8765", "", true},
		{"0.0.0.0:8765", "", false},
		{"0.0.0.0:8765", "t", true},
		{"192.168.1.5:8765", "", false},
	}
	for _, c := range cases {
		if err := checkExposure(c.addr, c.token); (err == nil) != c.ok {
			t.Errorf("checkExposure(%q, %q) = %v, want ok=%v", c.addr, c.token, err, c.ok)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "qdrant-api-key")
	_ = os.WriteFile(key, []byte("k3y\n"), 0o600)
	env := filepath.Join(dir, "endpoints.env")
	_ = os.WriteFile(env, []byte(`# comment
AI_INFERENCE_BASE_URL=http://127.0.0.1:11434/v1
AI_MODEL_EMBED=ai:embed
AI_MODEL_EMBED_SOURCE=nomic-embed-text:latest
AI_EMBED_DIMS=768
QDRANT_GRPC=127.0.0.1:6334
QDRANT_API_KEY_FILE=`+key+"\n"), 0o644)

	cfg, err := loadConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmbedModel != "ai:embed" || cfg.EmbedDims != 768 || cfg.QdrantAPIKey != "k3y" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if got := collectionName("notes", cfg.EmbedSource, cfg.EmbedDims); got != "notes__nomic-embed-text-latest__768" {
		t.Errorf("collection = %s", got)
	}

	_ = os.WriteFile(env, []byte("AI_EMBED_DIMS=abc\n"), 0o644)
	if _, err := loadConfig(env); err == nil {
		t.Error("incomplete file: want error")
	}
}
