package main

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Embedder turns text into a vector. The interface keeps notes.go testable
// without a model server.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// openAIEmbedder calls /v1/embeddings on any OpenAI-compatible server
// (Ollama, llama.cpp, vLLM...). This is the only model API the server uses.
type openAIEmbedder struct {
	client openai.Client
	model  string
	dims   int
}

func newOpenAIEmbedder(baseURL, model string, dims int) *openAIEmbedder {
	return &openAIEmbedder{
		// Local servers ignore the key, but the client requires one.
		client: openai.NewClient(option.WithBaseURL(baseURL), option.WithAPIKey("local")),
		model:  model,
		dims:   dims,
	}
}

func (e *openAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(e.model),
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
	})
	if err != nil {
		return nil, fmt.Errorf("embed with %s: %w", e.model, err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embed with %s: empty response", e.model)
	}

	raw := resp.Data[0].Embedding
	if len(raw) != e.dims {
		return nil, fmt.Errorf("embed with %s: got %d dimensions, endpoints file says %d", e.model, len(raw), e.dims)
	}

	// Qdrant stores float32; the API returns float64.
	vec := make([]float32, len(raw))
	for i, v := range raw {
		vec[i] = float32(v)
	}
	return vec, nil
}
