package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Config is what the server needs from the AI stack. It comes only from
// /etc/ai-stack/endpoints.env (written by infra/ai), so the server works with
// whatever inference backend is installed.
type Config struct {
	InferenceBaseURL string // OpenAI-compatible root, e.g. http://127.0.0.1:11434/v1
	EmbedModel       string // stable alias to request, e.g. ai:embed
	EmbedSource      string // model the alias points at, used to name the collection
	EmbedDims        int
	QdrantGRPC       string // host:port
	QdrantAPIKey     string // empty when Qdrant has no key
}

// parseEnvFile reads KEY=VALUE lines, skipping blanks and # comments.
// Values are taken literally (no quotes or variable expansion), like systemd.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		env[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return env, sc.Err()
}

// loadConfig builds a Config from the endpoints file.
func loadConfig(path string) (Config, error) {
	env, err := parseEnvFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read endpoints file: %w", err)
	}

	for _, key := range []string{"AI_INFERENCE_BASE_URL", "AI_MODEL_EMBED", "AI_MODEL_EMBED_SOURCE", "AI_EMBED_DIMS", "QDRANT_GRPC"} {
		if env[key] == "" {
			return Config{}, fmt.Errorf("%s: %s is missing or empty", path, key)
		}
	}

	dims, err := strconv.Atoi(env["AI_EMBED_DIMS"])
	if err != nil || dims <= 0 {
		return Config{}, fmt.Errorf("%s: AI_EMBED_DIMS=%q is not a positive integer", path, env["AI_EMBED_DIMS"])
	}

	cfg := Config{
		InferenceBaseURL: env["AI_INFERENCE_BASE_URL"],
		EmbedModel:       env["AI_MODEL_EMBED"],
		EmbedSource:      env["AI_MODEL_EMBED_SOURCE"],
		EmbedDims:        dims,
		QdrantGRPC:       env["QDRANT_GRPC"],
	}

	if keyFile := env["QDRANT_API_KEY_FILE"]; keyFile != "" {
		b, err := os.ReadFile(keyFile)
		if err != nil {
			return Config{}, fmt.Errorf("read Qdrant API key: %w", err)
		}
		cfg.QdrantAPIKey = strings.TrimSpace(string(b))
	}
	return cfg, nil
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]+`)

// collectionName follows the stack's rule: <base>__<embedding model>__<dims>.
// A different embedding model gets a different collection, because vectors
// from different models can't be compared.
func collectionName(base, embedSource string, dims int) string {
	return fmt.Sprintf("%s__%s__%d", base, nonAlnum.ReplaceAllString(embedSource, "-"), dims)
}
