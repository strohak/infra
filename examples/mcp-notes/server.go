package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool inputs and outputs. The SDK turns these structs into the JSON Schemas
// the model sees, so the `jsonschema` descriptions are effectively prompts:
// write them for the model.

type AddNoteIn struct {
	Text string   `json:"text" jsonschema:"the note to remember, in plain language"`
	Tags []string `json:"tags,omitempty" jsonschema:"optional short labels, e.g. [\"homelab\", \"todo\"]"`
}

type AddNoteOut struct {
	Note Note `json:"note"`
}

type SearchNotesIn struct {
	Query string `json:"query" jsonschema:"what to look for; matched by meaning, not exact words"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum results (default 5, max 50)"`
}

type SearchNotesOut struct {
	Results []Hit `json:"results"`
}

// newMCPServer registers the tools. Returning an error from a handler makes
// the SDK send it to the model as a tool error, which it can read and react to.
func newMCPServer(notes *Notes) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "mcp-notes", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_note",
		Description: "Save a note to the user's personal notes so it can be found later by meaning.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AddNoteIn) (*mcp.CallToolResult, AddNoteOut, error) {
		note, err := notes.Add(ctx, in.Text, in.Tags)
		if err != nil {
			return nil, AddNoteOut{}, err
		}
		return nil, AddNoteOut{Note: note}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_notes",
		Description: "Search the user's personal notes by meaning. Use this before answering questions about things the user saved.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchNotesIn) (*mcp.CallToolResult, SearchNotesOut, error) {
		hits, err := notes.Search(ctx, in.Query, in.Limit)
		if err != nil {
			return nil, SearchNotesOut{}, err
		}
		if hits == nil {
			hits = []Hit{}
		}
		return nil, SearchNotesOut{Results: hits}, nil
	})

	return server
}

// newHTTPHandler serves MCP over Streamable HTTP at /mcp (what Open WebUI,
// Claude Code and the go-sdk client speak) plus a /healthz probe.
func newHTTPHandler(server *mcp.Server, token string) http.Handler {
	mux := http.NewServeMux()
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux.Handle("/mcp", requireBearer(token, mcpHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

// requireBearer checks "Authorization: Bearer <token>" when a token is set.
// With no token configured it allows everything, which is only safe on
// 127.0.0.1 (main refuses a non-loopback address without a token).
func requireBearer(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-notes"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
