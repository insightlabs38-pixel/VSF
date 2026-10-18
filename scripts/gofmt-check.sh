#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
unformatted="$(gofmt -l "$root/backend")"
if [ -n "$unformatted" ]; then
  echo "gofmt needed:" >&2
  echo "$unformatted" >&2
  exit 1
fi
