# llm-stack

Local LLM inference plus tracing for the `chat-harness` learning project:

| Component | Runs as | URL |
|---|---|---|
| **Ollama** | native systemd service | http://localhost:11434 |
| **Langfuse** (web UI + API) | Docker Compose | http://localhost:3000 |
| Langfuse worker, Postgres, ClickHouse, Redis, MinIO | Docker Compose | localhost only |

Ollama runs on the host instead of in a container so it can use the GPU
directly (on WSL2, through the Windows NVIDIA driver) without the NVIDIA
container toolkit.

```
 chat-harness (Go) ──chat──▶ Ollama :11434
        │
        └──OTLP traces──▶ Langfuse :3000/api/public/otel
                              │
              worker ─ Postgres ─ ClickHouse ─ Redis ─ MinIO
```

---

## Prerequisites

- `workstation/playbook.yml` has been run, so Docker Engine and the compose plugin are installed.
- **WSL2 memory:** WSL2 gets only part of the Windows host's RAM by default.
  Langfuse uses about 4–6 GB at idle, plus whatever the model needs. If
  containers get OOM-killed or Ollama is slow, raise the limit in
  `%UserProfile%\.wslconfig` on the Windows side:

  ```ini
  [wsl2]
  memory=16GB
  ```

  Then run `wsl --shutdown` in PowerShell and reopen WSL.

---

## Setup

```bash
cd ~/code/infra/llm-stack
ansible-playbook playbook.yml --ask-become-pass
```

This will:

1. Install Ollama, using the official install script, if it isn't installed.
2. Pull the models in `ollama_models` (default `llama3.2:3b`).
3. On first run, generate `.env` with random secrets and a pre-created
   Langfuse org, project, API key pair and admin user.
4. Run `docker compose up -d --wait` and wait for Langfuse's health endpoint.

It is safe to re-run. Pick different models without editing the file:

```bash
ansible-playbook playbook.yml --ask-become-pass -e '{"ollama_models": ["llama3.1:8b", "qwen2.5:7b"]}'
```

To install and pull only, without starting Langfuse: add `-e llm_stack_start=false`.

---

## Day-to-day

```bash
cd ~/code/infra/llm-stack
docker compose ps              # what's running
docker compose stop            # free the RAM; data is kept
docker compose start           # bring it back
docker compose logs -f langfuse-web langfuse-worker
```

The containers use `restart: unless-stopped`: after `docker compose stop`
they stay stopped across reboots. Otherwise they come back up with Docker.

Ollama is a systemd service: `systemctl status ollama`, `journalctl -u ollama -f`.

```bash
ollama list                    # pulled models
ollama run llama3.2:3b         # quick chat in the terminal
ollama ps                      # loaded models, and whether on GPU or CPU
```

---

## Credentials

Everything is in `.env`, which is gitignored and created with mode 0600:

| What | Variable |
|---|---|
| UI login | `LANGFUSE_INIT_USER_EMAIL` / `LANGFUSE_INIT_USER_PASSWORD` |
| API public key | `LANGFUSE_INIT_PROJECT_PUBLIC_KEY` (`pk-lf-…`) |
| API secret key | `LANGFUSE_INIT_PROJECT_SECRET_KEY` (`sk-lf-…`) |

The `LANGFUSE_INIT_*` values are applied **only on the first start**, while
the database is empty. Editing them later changes nothing. Make changes in the UI instead.

### Sending traces from the harness (OpenTelemetry)

Langfuse accepts OTLP/HTTP traces at `http://localhost:3000/api/public/otel`,
authenticated with HTTP Basic using `public_key:secret_key`:

```bash
source .env
export OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:3000/api/public/otel"
export OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic $(printf '%s:%s' "$LANGFUSE_INIT_PROJECT_PUBLIC_KEY" "$LANGFUSE_INIT_PROJECT_SECRET_KEY" | base64 -w0)"
```

The Go OTel SDK (`otlptracehttp`) reads both variables automatically.

---

## Resetting

To wipe all traces, users and keys and start fresh:

```bash
docker compose down -v         # -v deletes the data volumes
rm .env
ansible-playbook playbook.yml --ask-become-pass
```

Delete `.env` **only together with** the volumes. The databases were
initialised with the old passwords and won't accept new ones.

---

## Optional: Langfuse playground → Ollama

Langfuse's playground and LLM-as-judge evals can call Ollama through its
OpenAI-compatible API. `langfuse-web` already maps `host.docker.internal`
to the host. However, Ollama listens only on 127.0.0.1 by default, which
containers can't reach. To allow it:

```bash
sudo systemctl edit ollama
# add:
#   [Service]
#   Environment="OLLAMA_HOST=0.0.0.0"
sudo systemctl restart ollama
```

Then add an LLM connection in the Langfuse UI with base URL
`http://host.docker.internal:11434/v1` and any non-empty API key.

> On a bare-metal machine this exposes Ollama, which has no authentication,
> on your LAN. Use a firewall rule, or skip this step.

---

## Updating

- **Langfuse:** run `docker compose pull && docker compose up -d`. The
  `langfuse_image_tag` var (default `4`) follows the latest 4.x release. To pin
  an exact version, set `LANGFUSE_IMAGE_TAG` in `.env`.
- **compose.yml vs upstream:** the header of `compose.yml` lists what
  differs from Langfuse's official file and has a one-line `diff` command.
- **Ollama:** `curl -fsSL https://ollama.com/install.sh | sh`

## Ports in use

All bound to 127.0.0.1: 3000 (Langfuse), 3030 (worker), 5432 (Postgres),
6379 (Redis), 8123/9000 (ClickHouse), 9090/9091 (MinIO), 11434 (Ollama).
If you already run Postgres or Redis locally, change the left-hand host port
in `compose.yml`.
