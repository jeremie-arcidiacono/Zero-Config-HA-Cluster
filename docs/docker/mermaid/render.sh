#!/bin/sh
# Render every Mermaid source file in the mounted directory to SVG.
#
# Example with the report assets directory mounted at /data:
#   docker run --rm -v docs/report/assets:/data tb/mermaid:local
#
# Reads  $SRC_DIR/*.mmd      (default /data/diagrams-src)
# Writes $OUT_DIR/<name>.svg (default /data/diagrams)
#
# Environment overrides:
#   SRC_DIR, OUT_DIR   input/output directories
#   FORCE=1            re-render even when the SVG is newer than its source
set -eu

SRC_DIR="${SRC_DIR:-/data/diagrams-src}"
OUT_DIR="${OUT_DIR:-/data/diagrams}"


MMDC="/home/mermaidcli/node_modules/.bin/mmdc"
PUPPETEER_CONFIG="/puppeteer-config.json"

if [ ! -d "$SRC_DIR" ]; then
    echo "Source directory not found: $SRC_DIR" >&2
    exit 1
fi

mkdir -p "$OUT_DIR"

found=0
rendered=0
for src in "$SRC_DIR"/*.mmd; do
    [ -e "$src" ] || break  # no matches found

    found=$((found + 1))
    name="$(basename "$src" .mmd)"
    out="$OUT_DIR/$name.svg"

    if [ "${FORCE:-0}" != "1" ] && [ -f "$out" ] && [ "$out" -nt "$src" ]; then
        echo "up-to-date  $name.svg"
        continue
    fi

    echo "rendering   $name.svg"
    "$MMDC" -p "$PUPPETEER_CONFIG" -b transparent -i "$src" -o "$out"
    rendered=$((rendered + 1))
done

if [ "$found" -eq 0 ]; then
    echo "No .mmd files found in $SRC_DIR" >&2
    exit 1
fi

echo "Done: $rendered rendered, $((found - rendered)) up-to-date ($found total) in $OUT_DIR"
