package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"

	"github.com/qdrant/go-client/qdrant"
)

// Note is one stored note.
type Note struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// Hit is a search result.
type Hit struct {
	Note
	Score float32 `json:"score" jsonschema:"cosine similarity, higher is closer (max 1)"`
}

// Store keeps notes and their vectors. The interface lets tests use an
// in-memory fake instead of Qdrant.
type Store interface {
	Ensure(ctx context.Context) error
	Upsert(ctx context.Context, n Note, vec []float32) error
	Search(ctx context.Context, vec []float32, limit int) ([]Hit, error)
}

// qdrantStore talks to Qdrant over gRPC with the official Go client.
type qdrantStore struct {
	client     *qdrant.Client
	collection string
	dims       int
}

func newQdrantStore(grpcAddr, apiKey, collection string, dims int) (*qdrantStore, error) {
	host, portStr, err := net.SplitHostPort(grpcAddr)
	if err != nil {
		return nil, fmt.Errorf("QDRANT_GRPC %q: %w", grpcAddr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("QDRANT_GRPC %q: bad port", grpcAddr)
	}
	cfg := &qdrant.Config{Host: host, Port: port, APIKey: apiKey}
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		// The client warns that the key travels without TLS; on loopback it
		// never leaves the machine, so only show errors.
		cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	client, err := qdrant.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to Qdrant: %w", err)
	}
	return &qdrantStore{client: client, collection: collection, dims: dims}, nil
}

// Ensure creates the collection on first use.
func (s *qdrantStore) Ensure(ctx context.Context) error {
	exists, err := s.client.CollectionExists(ctx, s.collection)
	if err != nil {
		return fmt.Errorf("check collection %s: %w", s.collection, err)
	}
	if exists {
		return nil
	}
	return s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: s.collection,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(s.dims),
			Distance: qdrant.Distance_Cosine,
		}),
	})
}

func (s *qdrantStore) Upsert(ctx context.Context, n Note, vec []float32) error {
	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: s.collection,
		Wait:           qdrant.PtrOf(true),
		Points: []*qdrant.PointStruct{{
			Id:      qdrant.NewIDUUID(n.ID),
			Vectors: qdrant.NewVectors(vec...),
			Payload: qdrant.NewValueMap(map[string]any{
				"text":       n.Text,
				"tags":       n.Tags,
				"created_at": n.CreatedAt,
			}),
		}},
	})
	return err
}

func (s *qdrantStore) Search(ctx context.Context, vec []float32, limit int) ([]Hit, error) {
	points, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.collection,
		Query:          qdrant.NewQuery(vec...),
		Limit:          qdrant.PtrOf(uint64(limit)),
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return nil, err
	}

	hits := make([]Hit, 0, len(points))
	for _, p := range points {
		var tags []string
		for _, v := range p.GetPayload()["tags"].GetListValue().GetValues() {
			tags = append(tags, v.GetStringValue())
		}
		hits = append(hits, Hit{
			Note: Note{
				ID:        p.GetId().GetUuid(),
				Text:      p.GetPayload()["text"].GetStringValue(),
				Tags:      tags,
				CreatedAt: p.GetPayload()["created_at"].GetStringValue(),
			},
			Score: p.GetScore(),
		})
	}
	return hits, nil
}

func (s *qdrantStore) Close() error { return s.client.Close() }
