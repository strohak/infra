# infra
 
Infrastructure and workstation automation for `bones` (WSL2 dev environment).
 
---
 
## Fresh WSL2 Rebuild
 
### Prerequisites (Windows side, before touching WSL2)
 
- Have your SSH private key accessible (Bitwarden or `C:\Users\Sean\.ssh\`)
- Have your GitHub credentials ready
- For the AI stack: install the current NVIDIA driver **on Windows only** (never inside WSL — it breaks GPU passthrough)
---
 
### 1. Install a fresh WSL2 Ubuntu distro
 
In PowerShell:
 
```powershell
wsl --install -d Ubuntu
```
 
Set your username and password when prompted.
 
---
 
### 2. Run bootstrap
 
```bash
bash <(curl -fsSL https://raw.githubusercontent.com/strohak/infra/master/workstation/bootstrap.sh)
```
 
This installs everything — Go, Podman, zsh, Starship (with a managed `starship.toml`), the GitHub CLI (`gh`), Zellij, all Go tools, and your directory structure. Takes a few minutes.

On a bare-metal Ubuntu-family desktop (detected via the kernel string, as opposed to a WSL2 shell), it also installs VS Code and a curated set of extensions.

The local AI stack (Ollama + Qdrant + Open WebUI) is separate and opt-in — see [AI stack](#ai-stack-ollama--qdrant--open-webui) below.
 
---
 
### 3. Restore your SSH key
 
```bash
mkdir -p ~/.ssh
 
# Paste your private key from Bitwarden into this file
nano ~/.ssh/id_ed25519
 
# Set correct permissions
chmod 700 ~/.ssh
chmod 600 ~/.ssh/id_ed25519
 
# Test GitHub connection
ssh -T git@github.com
```
 
---
 
### 4. Restore your dotfiles
 
```bash
git clone --bare git@github.com:strohak/dotfiles.git ~/.dotfiles
 
alias dotfiles='git --git-dir=$HOME/.dotfiles/ --work-tree=$HOME'
 
dotfiles checkout
 
dotfiles config status.showUntrackedFiles no
```
 
If checkout complains about existing files:
 
```bash
mkdir -p ~/.dotfiles-backup
dotfiles checkout 2>&1 | grep "^\s" | awk '{print $1}' | xargs -I{} mv {} ~/.dotfiles-backup/{}
dotfiles checkout
```
 
---
 
### 5. Apply the new shell
 
```bash
exec zsh
```
 
---
 
**Done.** Full environment restored.
 
---
 
## Repository Structure
 
```
infra/
├── README.md              # this file
├── workstation/
│   ├── bootstrap.sh       # run on a fresh WSL2 install to kick off setup
│   ├── playbook.yml       # Ansible playbook — idempotent, safe to re-run
│   └── files/
│       └── starship.toml  # Starship prompt config, copied to ~/.config/
├── ai/                    # local AI stack (inference + Qdrant + Open WebUI), any host type
│   ├── playbook.yml
│   ├── inventory/         # host types: ai_local, ai_servers (+ group_vars)
│   ├── roles/             # host_facts, inference, inference_ollama, qdrant, open_webui,
│   │                      # podman_service, ai_endpoints, inference_verify
│   └── tests/             # contract tests: stub backend + real Qdrant
├── examples/
│   └── mcp-notes/         # example MCP tool server in Go (template for your own tools)
├── docs/
│   └── ai-local-stack.md  # design + decisions for the AI stack
└── .github/workflows/
    └── ai-stack.yml       # lint, Go tests, contract tests, manual full-stack run
```
 
---
 
## Re-running the Playbook
 
Safe to run at any time to update tools or verify environment state:
 
```bash
cd ~/code/infra/workstation
ansible-playbook playbook.yml --ask-become-pass
```
 
## Updating Go
 
Change the `go_version` variable at the top of `playbook.yml` and re-run the playbook.

## Updating Go tools

Each entry in `go_tools` in `playbook.yml` pins a version. Bump `version` and
re-run the playbook. A tool is only reinstalled when its installed binary
doesn't match the pin, so re-runs are quiet otherwise.

Tools are built with `GOTOOLCHAIN=local`, so if a new tool release needs a
newer Go than `go_version`, the install fails instead of silently downloading
another toolchain. Bump `go_version` first in that case.

`golangci-lint` is on v2. Older v1 `.golangci.yml` files can be converted with
`golangci-lint migrate`.

## Updating Zellij

Change the `zellij_version` variable at the top of `playbook.yml` and re-run the playbook.

## AI stack (Ollama + Qdrant + Open WebUI)

A local inference server, the Qdrant vector DB and the Open WebUI chat UI, for agents, RAG,
scripts and your own MCP tools.
Design, decisions and the backend contract: [docs/ai-local-stack.md](docs/ai-local-stack.md).

```bash
cd ~/code/infra
# This machine (bones, a desktop...). Drop -K when running as root.
ansible-playbook -i ai/inventory/hosts.yml ai/playbook.yml --limit localhost -K
# A LAN server defined in ai/inventory/hosts.yml
ansible-playbook -i ai/inventory/hosts.yml ai/playbook.yml --limit gpu-box -K --ask-vault-pass
# Or as part of the workstation playbook
ansible-playbook workstation/playbook.yml --ask-become-pass -e ai_stack_enabled=true
```

On WSL2 the first run enables systemd in `/etc/wsl.conf` and stops: run `wsl --shutdown`
in PowerShell, reopen Ubuntu and run it again.

It detects the host (WSL2 / container / bare metal, NVIDIA / AMD / CPU, VRAM), picks a
model tier that fits, and ends with a contract check: a real tool call, an embedding, and a
Qdrant store + search. Everything binds to `127.0.0.1` unless the host is in `ai_servers`.

**Using it:** everything a client needs is in `/etc/ai-stack/endpoints.env`
(`AI_INFERENCE_BASE_URL`, `AI_MODEL_CHAT`, `AI_MODEL_EMBED`, `QDRANT_URL`, …). Talk only
the OpenAI-compatible API at that URL so the backend stays swappable:

```bash
source /etc/ai-stack/endpoints.env
curl -s "$AI_INFERENCE_BASE_URL/chat/completions" -H 'Content-Type: application/json' \
  -d "{\"model\":\"$AI_MODEL_CHAT\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
```

Ask for the stable aliases (`ai:chat`, `ai:fast`, `ai:embed`), not model tags: they keep
working when a tier's model or the backend changes.

From a browser (on WSL2, the Windows one works too):

- **Open WebUI:** <http://localhost:3000>. **Create your account first**, because the first account becomes admin.
- **Qdrant dashboard:** <http://localhost:6333/dashboard>.

**Your own tools:** write them as MCP servers in Go. [`examples/mcp-notes`](examples/mcp-notes) is a
working template (add/search notes over `ai:embed` + Qdrant), with steps to register it in Open
WebUI or Claude Code.

| To change | Edit, then re-run |
|---|---|
| Ollama version | `ollama_version` in `ai/roles/inference_ollama/defaults/main.yml` |
| Qdrant version | `qdrant_version` in `ai/roles/qdrant/defaults/main.yml` (data is kept) |
| Open WebUI version, or turn it off | `open_webui_version` / `open_webui_enabled` in `ai/roles/open_webui/defaults/main.yml` |
| MCP tools pre-registered in Open WebUI | `open_webui_mcp_servers` (applied on its first start) |
| Models per VRAM tier | `ai_model_tiers` in `ai/roles/inference/defaults/main.yml` |
| Models for one host/group | `ai_models` in `ai/inventory/group_vars/` or `host_vars/` |
| Context length, KV cache, keep-alive | `ollama_env` in `ai/roles/inference_ollama/defaults/main.yml` |
| Inference backend | `inference_backend` — see "Adding a backend" in the design doc |

Check services with `systemctl status ai-inference` and `systemctl --user status qdrant open-webui`
(`sudo systemctl status qdrant open-webui` when installed as root).

**Tests:** CI (`.github/workflows/ai-stack.yml`) lints everything, runs the Go tests and runs the
contract tests on every push. The **full-stack** job (Actions → ai-stack → Run workflow) installs
the real stack on a fresh Ubuntu VM. To run the contract tests locally, see [Tests and
CI](docs/ai-local-stack.md#tests-and-ci).

## Known machine-specific items (not automated)

These are real, in-use pieces of infrastructure on some machines but are intentionally
out of scope for this playbook — they're either personal preference, tied to a specific
box's storage/network layout, or both. Documented here so they aren't rediscovered from
scratch:

- **NordVPN** — personal VPN client, installed manually per machine.
- **Samba** — LAN file sharing, configured manually with host-specific settings.
- **OpenVPN** — separate from NordVPN, purpose is machine-specific.
- **ZFS + Snapper + Timeshift + restic** — filesystem snapshotting and backup stack,
  tied to a specific disk/pool layout and `systemd` timers set up by hand.

If any of these need to become reproducible later, they belong in their own
machine-specific playbook or role rather than this general dev-workstation one.
