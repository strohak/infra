# Local AI stack: design and decisions

Status: accepted · First target: `bones` (WSL2 Ubuntu, NVIDIA, 8–12 GB VRAM) · Nov 2026

## Context

The goal is a local AI stack that serves four uses:

1. A coding agent (read/edit files, run commands, in a tool-calling loop).
2. Chat and RAG over personal docs and repos.
3. Scripting/automation (summarize, classify, extract from CLI tools).
4. A learning project: understanding how agents, tool calling and inference actually work.

Two requirements shape everything below:

- **Install anywhere.** WSL2 today, but also bare-metal desktops, a dedicated LAN
  server, NVIDIA / AMD / CPU-only, and host types that don't exist yet.
- **Swappable inference.** Ollama is the first backend, but replacing it
  (llama.cpp, vLLM, …) must not touch the harness or the vector store.

The Ansible lives in [`ai/`](../ai); the Go harness will live in its own repo.

## Architecture

```
            ┌──────────── consumers (backend-agnostic) ────────────┐
            │  Go harness · scripts · shells                       │
            │  read /etc/ai-stack/endpoints.env, nothing else      │
            └───────┬──────────────────────────────┬───────────────┘
   OpenAI-compatible│/v1                    REST/gRPC│
            ┌───────▼────────┐             ┌────────▼────────┐
            │ ai-inference   │             │ Qdrant          │
            │ (ollama today) │             │ Podman/Quadlet  │
            └────────────────┘             └─────────────────┘
```

Ansible roles, in run order:

| Role | Job |
|---|---|
| `host_facts` | Detects platform (wsl/container/baremetal), systemd, accelerator (nvidia/amd/cpu), VRAM, arch. Changes nothing. |
| `inference` | Backend-neutral dispatcher: validates `inference_backend`, picks the model tier, resolves model ids, runs `inference_<backend>`, checks the backend published its endpoints. |
| `inference_ollama` | The Ollama implementation of the contract. |
| `qdrant` | Qdrant in Podman as a systemd service. |
| `ai_endpoints` | Writes `/etc/ai-stack/endpoints.env` (and the Qdrant key file). |
| `inference_verify` | Contract check through the endpoints file only. |

## The inference contract

A backend role `inference_<name>` must:

1. **Serve the OpenAI-compatible API** at `http://<addr>:<port>/v1`, specifically:
   - `GET /models`;
   - `POST /chat/completions` (streaming and `tools`);
   - `POST /embeddings`.
2. **Serve the three logical models.** For each role in `ai_model_tiers` (`chat`, `fast`, `embed`), it reads only its own column, e.g. `chat: { ollama: "qwen3:8b", llamacpp: "…" }`. The ids must match exactly what `/v1/models` reports.
3. **Run under systemd** with the alias `ai-inference.service`, so `systemctl status ai-inference` works whatever the backend.
4. **Set two facts:** `inference_base_url` (ending in `/v1`) and `inference_health_url`.
5. **Keep tuning server-side.** Context length, KV-cache type and keep-alive go in the backend's own service config, never in requests. That's what lets clients use the plain OpenAI API with no backend-specific parameters.

`inference_verify` then proves the contract. Using only `endpoints.env`, it checks:

- every model is listed by `/v1/models`;
- the chat model returns a `get_weather` **tool call**;
- the fast model answers;
- embeddings have exactly `AI_EMBED_DIMS` dimensions;
- the embedding can be **stored in Qdrant and found again** by vector search.

**A backend counts as dropped in once this passes.**

### Adding a backend (e.g. llama.cpp)

1. Create `ai/roles/inference_llamacpp/` (defaults, tasks, templates, handlers). Install
   `llama-server`, run it under systemd with the `ai-inference` alias, load the models,
   and set `inference_base_url` / `inference_health_url`.
2. Add `llamacpp` to `inference_supported_backends` and its default port to
   `inference_default_ports` (`ai/roles/inference/defaults/main.yml`).
3. Add a `llamacpp:` column to every model role in `ai_model_tiers`. Keep the same
   embedding model, or note the re-index below.
4. Run with `-e inference_backend=llamacpp`. The contract check tells you what's missing.

**Changing the embedding model means re-indexing.** Vectors from different models can't be
mixed. Collection names should include model and size, e.g. `docs__nomic-embed-text__768`,
so a mismatch fails loudly instead of returning nonsense.

### endpoints.env

```
AI_INFERENCE_BACKEND=ollama
AI_INFERENCE_BASE_URL=http://127.0.0.1:11434/v1
AI_INFERENCE_HEALTH_URL=http://127.0.0.1:11434/api/version
AI_MODEL_TIER=8gb
AI_MODEL_CHAT=qwen3:8b
AI_MODEL_FAST=gemma4:e4b
AI_MODEL_EMBED=nomic-embed-text:latest
AI_EMBED_DIMS=768
QDRANT_URL=http://127.0.0.1:6333
QDRANT_GRPC=127.0.0.1:6334
QDRANT_API_KEY_FILE=
```

## Decisions

### Inference server: Ollama (v0.40.14)

| Option | Verdict |
|---|---|
| **Ollama** | **Chosen.** Simplest CUDA-on-WSL2 path: it bundles its own CUDA runtime and uses the Windows driver's passthrough, so no Linux driver is needed. Handles model downloads, and has a stable OpenAI-compatible `/v1`. Very active (releases weekly). |
| llama.cpp `llama-server` | First drop-in candidate. More control (exact quantization, sampling, speculative decoding) and the engine Ollama is built around, but you manage GGUF files yourself. |
| vLLM | Best throughput for many concurrent users, but needs Python/CUDA setup and more VRAM than 12 GB. Revisit on a bigger server. |
| LM Studio | GUI-first, Windows-side. Can't be provisioned headless with Ansible. |

GPU drivers are **never** installed by these roles. On WSL2, a Linux NVIDIA driver breaks
passthrough. On bare metal, the driver belongs to the host owner. Without a driver,
everything still works, on CPU.

### Models: tiers by VRAM

Each tier is the first that fits the detected VRAM of the largest single GPU; CPU-only hosts always get `cpu`. Set `ai_models` in inventory to override.

| Tier | Min VRAM | chat | fast | embed |
|---|---|---|---|---|
| 24gb | 23000 MiB | `qwen3.6:27b` | `qwen3:8b` | `nomic-embed-text` (768) |
| 16gb | 15500 MiB | `gpt-oss:20b` | `qwen3:8b` | `nomic-embed-text` (768) |
| 8gb (bones) | 7500 MiB | `qwen3:8b` | `gemma4:e4b` | `nomic-embed-text` (768) |
| cpu | — | `qwen3:4b` | `qwen3:4b` | `nomic-embed-text` (768) |

Why these models:

- **`qwen3:8b`** is the most-recommended 8–12 GB model for reliable tool calling.
- **`gemma4:e4b`** adds a fast general model with vision.
- **`nomic-embed-text`** is the most widely used local embedding model.

These picks come from community comparisons (sources below), not our own benchmarks. Change
them freely: the contract check will flag a model that doesn't call tools.

Server tuning (Ollama):

- `OLLAMA_CONTEXT_LENGTH=16384`, because agents need long context.
- `OLLAMA_FLASH_ATTENTION=1` + `OLLAMA_KV_CACHE_TYPE=q8_0` roughly halves KV-cache memory.
- `OLLAMA_KEEP_ALIVE=30m` keeps models loaded between requests.

### Vector store: Qdrant (v1.20.0)

| Option | Verdict |
|---|---|
| **Qdrant** | **Chosen.** A real vector DB with payload filtering, a web dashboard for inspecting collections, an official Go client (`github.com/qdrant/go-client` v1.20.1, gRPC) and a single container. |
| pgvector | Good if Postgres is already in the stack; it isn't, and running Postgres just for vectors is more to maintain. |
| chromem-go / sqlite-vec | Embedded and appealing, but neither Go package has had a release since 2024. |

How it runs: a Podman **Quadlet** unit. It's rootless under `systemd --user` with linger for a
normal user, and a system service when run as root. Data is kept in the named volume
`qdrant-data`, so upgrades are just a version bump.

### Harness: Go, no framework (yet)

- **Language: Go.** It's the main language here, it builds a single static binary that runs on any host, and a harness is mostly HTTP + JSON + a loop. Go's thinner ML ecosystem doesn't matter for that.
- **Model client: `github.com/openai/openai-go/v3` only,** pointed at `AI_INFERENCE_BASE_URL`. Using the generic API is what makes backend swaps invisible to the harness. Don't import Ollama's own client.
- **Vector client:** `github.com/qdrant/go-client`.
- **Tools / MCP:** `github.com/modelcontextprotocol/go-sdk` (v1.8.0) as an MCP *client*, so existing MCP servers become tools.
- **Seams:** small `LLM`, `Embedder` and `VectorStore` interfaces inside the harness, so tests can use fakes.
- **No framework at first.** Writing the agent loop by hand *is* the learning goal. Revisit Eino, Genkit Go or LangChainGo once the hand-written loop works and their abstractions would save real effort.

Roadmap:

1. **Chat CLI:** stream tokens from `AI_MODEL_CHAT`.
2. **Tool loop:** define tools, send them, execute the `tool_calls`, feed the results back, repeat.
3. **Coding agent:** read/write/edit-file and run-command tools. Ask before writes, and run commands in a throwaway Podman container with the repo mounted.
4. **RAG:**
   - chunk files;
   - embed with `AI_MODEL_EMBED`;
   - upsert into a `<name>__<model>__<dims>` collection;
   - retrieve and stuff the results into the prompt.
5. **MCP client:** load tools from MCP servers.

## Host types

| Type | How | Notes |
|---|---|---|
| WSL2 (bones) | `--limit localhost` | The role sets `systemd=true` in `/etc/wsl.conf`; the first run asks for a `wsl --shutdown`. Windows apps reach `localhost:11434` / `:6333` through WSL's localhost forwarding. |
| Bare-metal desktop | `--limit localhost` | Install the GPU driver first (or run on CPU). |
| LAN server | an `ai_servers` host over SSH | Binds `0.0.0.0`. A Qdrant API key is required (vault). Ollama has no auth, so firewall the port to your LAN or front it with an authenticating proxy. |
| Container | as root | `host_facts` reports `container`. Services need systemd running in the container. |

Adding a new type:

- A new **inventory group** plus `group_vars/<group>/` when it only needs different settings.
- A new `host_facts/tasks/<os_family>.yml`, plus install tasks in each role, for a new OS family. Only Debian-family systems are supported today, and anything else fails with a message saying so.

## Sources

- Ollama release v0.40.14 and Go module versions: checked against `proxy.golang.org` (Nov 2026).
- Qdrant v1.20.0 image: Docker Hub `qdrant/qdrant` tags (Oct 21, 2026).
- [Ollama release notes](https://ollamaherd.com/guides/ollama-release-notes) · [Ollama (Wikipedia)](https://en.wikipedia.org/wiki/Ollama)
- WSL2 + Ollama setup: [insiderllm.com guide](https://insiderllm.com/guides/wsl2-ollama-windows-setup-guide/) · [itecsonline.com](https://itecsonline.com/post/ollama-ubuntu-nvidia)
- Model picks: [localaimaster.com: best Ollama models for agents](https://localaimaster.com/blog/best-ollama-models-for-agents) · [VRAM table](https://localaimaster.com/blog/ollama-model-ram-vram-table) · [haimaker.ai VRAM tiers](https://haimaker.ai/blog/ollama-coding-models-by-nvidia-rtx-vram/) · [webscraft.org tool-calling comparison](https://webscraft.org/blog/yaku-model-ollama-obrati-dlya-agenta-z-tool-calling-porivnyannya-i-benchmarki?lang=en)
- Go frameworks: [xavidop.me: Go GenAI frameworks 2026](https://xavidop.me/genkit/2026-09-21-top-go-genai-frameworks-2026/) · [encore.dev](https://encore.dev/articles/best-go-framework-for-building-ai-agents)
- Vector DBs: [techsy.io: Qdrant vs Chroma vs pgvector](https://techsy.io/en/blog/qdrant-vs-chroma-vs-pgvector)
