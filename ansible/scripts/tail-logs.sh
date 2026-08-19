#!/usr/bin/env bash
#
# Tail the antsd systemd logs of every active inventory node in a single tmux
# window, one pane per node.
# Meant to be run from Linux/WSL via `make logs`.
#
# The host list is read from the live Ansible inventory.
# Connection settings mirror ansible.cfg.
#
# Requirements : tmux, ansible, python3

set -euo pipefail

SESSION="antsd-logs"
SSH_USER="${SSH_USER:-ants}"

# Resolve paths relative to this script so they don't depend on the caller's cwd.
ANSIBLE_DIR="$(cd "$(dirname "$0")/.." && pwd)"   # scripts/ -> ansible/
KEY="${SSH_KEY:-$ANSIBLE_DIR/../keys/id_ed25519_ants-os-default}"
cd "$ANSIBLE_DIR"   # so `ansible-inventory` picks up ansible.cfg

# --- Preflight -------------------------------------------------------------
for cmd in tmux ansible-inventory python3; do
    command -v "$cmd" >/dev/null 2>&1 || {
        echo "error: required command '$cmd' not found in PATH" >&2
        exit 1
    }
done

# Already running? Just re-attach instead of building a second session.
if tmux has-session -t "$SESSION" 2>/dev/null; then
    exec tmux attach -t "$SESSION"
fi

# --- Discover active hosts -------------------------------------------------
hosts="$(ansible-inventory --list | python3 -c \
    'import sys,json;d=json.load(sys.stdin)["_meta"]["hostvars"];print("\n".join("%s %s"%(k,v.get("ansible_host",k)) for k,v in d.items()))')"

[ -n "$hosts" ] || { echo "error: no active hosts found in the inventory" >&2; exit 1; }

# --- Build the tmux session ------------------------------------------------
first=1
while read -r host ip; do
    [ -n "$host" ] || continue

    # A reconnect loop keeps the pane alive across node reboots. The journalctl
    # args and key path are single-quoted so they survive tmux's `sh -c`.
    cmd="while true; do \
        ssh -i '$KEY' -o StrictHostKeyChecking=no -o ConnectTimeout=5 \
            -o ServerAliveInterval=5 -o ServerAliveCountMax=2 \
            $SSH_USER@$ip 'journalctl -u antsd.service -n 100 -f'; \
        echo '-- $host disconnected, reconnecting in 2s --'; sleep 2; \
    done"

    if [ "$first" = 1 ]; then
        tmux new-session -d -s "$SESSION" -x 220 -y 50 "$cmd"
        first=0
    else
        tmux split-window -t "$SESSION" "$cmd"
        tmux select-layout -t "$SESSION" even-vertical  # make room for the next split
    fi
    tmux select-pane -t "$SESSION" -T "$host"
done <<< "$hosts"

# --- Look & feel, then attach ----------------------------------------------
tmux set-option    -t "$SESSION"    mouse on
tmux set-option -w -t "$SESSION"    pane-border-status top
tmux set-option -w -t "$SESSION"    pane-border-format ' #T '
tmux select-layout -t "$SESSION"    even-vertical       # default: full-width stacked rows

exec tmux attach -t "$SESSION"
