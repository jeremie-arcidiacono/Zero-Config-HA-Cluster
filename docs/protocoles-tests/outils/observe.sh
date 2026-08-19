#!/usr/bin/env bash
#
# Timeline of cluster states, timestamped by the local machine clock.
#
# Polls GET /status on a cluster machine and writes one CSV line per member per poll.
#
# Dependencies: bash, curl, python3.

set -uo pipefail

PORT=9000
INTERVAL=1
HOSTS="10.10.9.24,10.10.9.28"
OUT=""
ONCE=0

usage() {
    cat <<'EOF'
Usage: observe.sh [options]

  --hosts a,b,c     Observateurs interrogés dans l'ordre (défaut : 10.10.9.24,10.10.9.28)
  --port N          Port de l'interface d'administration (défaut : 9000)
  --interval N      Secondes entre deux sondages (défaut : 1)
  --out FICHIER     Écrit aussi le CSV dans ce fichier, en plus de la sortie standard
  --once            Un seul sondage puis sortie. Sert à vérifier l'installation
  --help            Affiche cette aide

Colonnes : ts_local,observer,observer_state,member,member_status,member_state

Exemples :
  ./observe.sh --once
  ./observe.sh --out ../resultats/2026-08-12-TB-01-run1/states.csv
  ./observe.sh --hosts 10.10.9.24,10.10.9.28,10.10.9.31 --interval 2
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --hosts)    HOSTS="$2"; shift 2 ;;
        --port)     PORT="$2"; shift 2 ;;
        --interval) INTERVAL="$2"; shift 2 ;;
        --out)      OUT="$2"; shift 2 ;;
        --once)     ONCE=1; shift ;;
        --help|-h)  usage; exit 0 ;;
        *)          echo "option inconnue : $1" >&2; usage >&2; exit 2 ;;
    esac
done

command -v curl >/dev/null 2>&1 || {
    echo "erreur : la commande 'curl' est absente du PATH" >&2
    exit 1
}

python3 -c "pass" >/dev/null 2>&1 || {
    echo "erreur : python3 est introuvable ou n'est pas exécutable (lancer ce script depuis WSL)" >&2
    exit 1
}

IFS=',' read -r -a HOST_LIST <<< "$HOSTS"
[ "${#HOST_LIST[@]}" -gt 0 ] || { echo "erreur : aucun observateur" >&2; exit 2; }

[ -n "$OUT" ] && mkdir -p "$(dirname "$OUT")"

# Write to stdout and, if requested, append to the file.
emit() {
    printf '%s\n' "$1"
    [ -n "$OUT" ] && printf '%s\n' "$1" >> "$OUT"
    return 0
}

TO_CSV='
import csv, json, sys

ts, observer = sys.argv[1], sys.argv[2]
try:
    status = json.load(sys.stdin)
except Exception:
    sys.exit(1)

writer = csv.writer(sys.stdout, quoting=csv.QUOTE_ALL, lineterminator="\n")
state = status.get("state", "")
members = status.get("members") or [{}]
for member in members:
    tags = member.get("tags") or {}
    writer.writerow([ts, observer, state,
                     member.get("name", ""), member.get("status", ""), tags.get("state", "")])
'

emit "ts_local,observer,observer_state,member,member_status,member_state"

# Poll observers in order and write a line per member.
# Return 1 when no observer replies.
poll() {
    local ts host json rows
    ts="$(date +%Y-%m-%dT%H:%M:%S.%3N%z)"

    for host in "${HOST_LIST[@]}"; do
        json="$(curl -s -m 2 "http://${host}:${PORT}/status" 2>/dev/null)" || continue
        [ -n "$json" ] || continue

        rows="$(printf '%s' "$json" | python3 -c "$TO_CSV" "$ts" "$host")" || continue
        [ -n "$rows" ] || continue

        emit "$rows"
        return 0
    done

    emit "\"${ts}\",\"NONE\",\"\",\"\",\"\",\"\""
    return 1
}

if [ "$ONCE" -eq 1 ]; then
    poll
    exit $?
fi

trap 'echo "-- arret de l observation --" >&2; exit 0' INT TERM

while true; do
    poll
    sleep "$INTERVAL"
done
