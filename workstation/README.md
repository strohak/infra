# infra
 
Infrastructure and workstation automation for `bones` (WSL2 dev environment).
 
---
 
## Fresh WSL2 Rebuild
 
### Prerequisites (Windows side, before touching WSL2)
 
- Have your SSH private key accessible (Bitwarden or `C:\Users\Sean\.ssh\`)
- Have your GitHub credentials ready
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
 
This installs everything — Go, Podman, zsh, Starship, all Go tools, and your directory structure. Takes a few minutes.
 
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
└── workstation/
    ├── README.md        # this file
    ├── bootstrap.sh     # run on a fresh WSL2 install to kick off setup
    └── playbook.yml     # Ansible playbook — idempotent, safe to re-run
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
