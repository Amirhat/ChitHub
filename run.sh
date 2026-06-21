#!/usr/bin/env bash
# Build (if needed) and launch ChitHub, scanning the parent folder.
set -euo pipefail
cd "$(dirname "$0")"

# Rebuild only when sources changed.
if [ ! -x ./chithub ] || [ -n "$(find . -name '*.go' -newer ./chithub 2>/dev/null)" ]; then
  echo "Building…"
  go build -o chithub .
fi

# Default root = the folder that contains this app (i.e. your repos folder).
ROOT="$(cd .. && pwd)"
exec ./chithub -root "$ROOT" "$@"
