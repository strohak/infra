#!/usr/bin/env bash
# bootstrap.sh — Run on a fresh WSL2 Ubuntu install to restore your workstation.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/strohak/infra/main/workstation/bootstrap.sh | bash
# or after cloning:
#   bash ~/code/infra/workstation/bootstrap.sh
 
set -euo pipefail
 
INFRA_REPO="https://github.com/strohak/infra"
INFRA_DIR="$HOME/code/infra"
 
echo ""
echo "╔══════════════════════════════════════╗"
echo "║   Workstation Bootstrap — bones      ║"
echo "╚══════════════════════════════════════╝"
echo ""
 
# --- Prereqs ---
echo "==> [1/4] Updating apt and installing Ansible..."
sudo apt update -qq
sudo apt install -y ansible git curl
 
# --- Clone infra repo (skip if already present) ---
echo "==> [2/4] Getting infra repo..."
if [ -d "$INFRA_DIR/.git" ]; then
    echo "    Already cloned — pulling latest..."
    git -C "$INFRA_DIR" pull --ff-only
else
    mkdir -p "$HOME/code"
    git clone "$INFRA_REPO" "$INFRA_DIR"
fi
 
# --- Run the playbook ---
echo "==> [3/4] Running workstation playbook..."
ansible-playbook "$INFRA_DIR/workstation/playbook.yml" --ask-become-pass
 
# --- Done ---
echo ""
echo "==> [4/4] Done."
echo ""
echo "Next steps:"
echo "  • Restore your SSH keys to ~/.ssh/ and set permissions (chmod 600 ~/.ssh/id_*)"
echo "  • Set up your dotfiles:  https://github.com/strohak/dotfiles"
echo "  • Restart your terminal or run: exec zsh"
echo ""
 
