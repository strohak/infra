// Command mcp-notes is an example MCP tool server for the local AI stack.
//
// It gives a model two tools, add_note and search_notes, backed by the stack's
// embedding model and Qdrant. It reads everything it needs from
// /etc/ai-stack/endpoints.env and only uses the OpenAI-compatible API, so it
// keeps working if the inference backend is swapped.
//
// Run:
//
//	go run . -addr 127.0.0.1:8765
//	MCP_NOTES_TOKEN=secret go run . -addr 0.0.0.0:8765   # non-loopback needs a token
//
// Then register http://127.0.0.1:8765/mcp in Open WebUI, Claude Code or your harness.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("mcp-notes stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:8765", "listen address")
	endpoints := flag.String("endpoints", envOr("AI_STACK_ENDPOINTS", "/etc/ai-stack/endpoints.env"), "AI stack endpoints file")
	base := flag.String("collection", "notes", "collection base name (model and size are appended)")
	flag.Parse()
	token := os.Getenv("MCP_NOTES_TOKEN")

	if err := checkExposure(*addr, token); err != nil {
		return err
	}

	cfg, err := loadConfig(*endpoints)
	if err != nil {
		return err
	}
	collection := collectionName(*base, cfg.EmbedSource, cfg.EmbedDims)

	store, err := newQdrantStore(cfg.QdrantGRPC, cfg.QdrantAPIKey, collection, cfg.EmbedDims)
	if err != nil {
		return err
	}
	defer store.Close()

	notes := NewNotes(newOpenAIEmbedder(cfg.InferenceBaseURL, cfg.EmbedModel, cfg.EmbedDims), store)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           newHTTPHandler(newMCPServer(notes), token),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("mcp-notes listening", "url", "http://"+*addr+"/mcp", "collection", collection,
		"embed", cfg.EmbedModel, "auth", token != "")

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

// checkExposure refuses to serve unauthenticated tools to the network.
func checkExposure(addr, token string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("-addr %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loopback && token == "" {
		return fmt.Errorf("-addr %s is reachable from other machines: set MCP_NOTES_TOKEN", addr)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
