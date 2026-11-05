# mcp-notes: example MCP tool server in Go

Gives a model two tools, backed by the local AI stack:

| Tool | Does |
|---|---|
| `add_note` | Embeds a note with the stack's embedding model (`ai:embed`) and stores it in Qdrant. Same text = same note (updated, not duplicated). |
| `search_notes` | Finds notes by meaning, not keywords. |

It's a template for your own tools. It uses the
[official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) over **Streamable HTTP**,
the transport Open WebUI, Claude Code and the Go SDK's client all speak. It reads only
`/etc/ai-stack/endpoints.env` and uses only the OpenAI-compatible API, so it keeps working
if the inference backend changes.

## Run

Requires the AI stack (`ai/playbook.yml`) on the same machine, and Go 1.27+.

```bash
cd ~/code/infra/examples/mcp-notes
go run . -addr 127.0.0.1:8765
# from another machine too (a token is required for any non-loopback address):
MCP_NOTES_TOKEN=$(openssl rand -hex 24) go run . -addr 0.0.0.0:8765
```

Flags:

| Flag | Default |
|---|---|
| `-addr` | `127.0.0.1:8765` |
| `-endpoints` | `/etc/ai-stack/endpoints.env`, or `$AI_STACK_ENDPOINTS` |
| `-collection` | `notes` |

The collection name gets the model and size appended (e.g. `notes__nomic-embed-text-latest__768`), so a new embedding model starts a fresh index instead of mixing incompatible vectors.

## Use it from a client

**Open WebUI.** Do either of these:
- Admin Settings → External Tools → **+** → type **MCP (Streamable HTTP)**, URL
  `http://127.0.0.1:8765/mcp`, plus a Bearer key if you set `MCP_NOTES_TOKEN`.
- Pre-register it in inventory (applied when Open WebUI first starts):

  ```yaml
  # ai/inventory/group_vars/all.yml
  open_webui_mcp_servers:
    - id: notes
      name: Notes
      description: Store and search personal notes
      url: http://127.0.0.1:8765/mcp
      # token: "{{ vault_mcp_notes_token }}"
  ```

Then, in a chat: **+** → Tools → enable Notes. Try "remember that the NAS backs up at 2am", then "when does the NAS back up?".

**Claude Code:**

```bash
claude mcp add --transport http notes http://127.0.0.1:8765/mcp
# with a token:
claude mcp add --transport http notes http://127.0.0.1:8765/mcp --header "Authorization: Bearer $MCP_NOTES_TOKEN"
```

**Your Go harness:** `mcp.NewClient(...).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ".../mcp"}, nil)`,
then `ListTools` / `CallTool`. `server_test.go` shows exactly this.

## Layout

| File | What's in it |
|---|---|
| `main.go` | Flags, config, wiring, HTTP server, graceful shutdown, refusal to expose without a token |
| `config.go` | Parses `endpoints.env`; the collection-naming rule |
| `embed.go` | `Embedder` interface + OpenAI-compatible implementation (`openai-go/v3`) |
| `store.go` | `Store` interface + Qdrant implementation (`qdrant/go-client`, gRPC) |
| `notes.go` | The logic (add/search). No MCP in here, so it's reusable elsewhere |
| `server.go` | MCP tool definitions, Streamable HTTP handler, bearer-token check |
| `*_test.go` | Fakes for `Embedder`/`Store`; tools exercised through a real MCP client in memory and over HTTP |

## Writing your own tool

1. Define input/output structs. The `jsonschema:"..."` tags become the descriptions the model
   reads, so write them as instructions to the model.
2. Register the tool with `mcp.AddTool(server, &mcp.Tool{Name, Description}, handler)`.
   Returning an `error` sends a tool error the model can read and recover from.
3. Keep the real work behind an interface (like `Embedder`/`Store`), so it can be tested without
   servers.
4. Test through `mcp.NewInMemoryTransports()`, the same protocol a client uses.

```bash
go test -race ./...
```
