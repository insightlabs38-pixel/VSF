#!/usr/bin/env bash
set -euo pipefail
git config --local core.hooksPath .githooks
git config --local user.name insightlabs38-pixel
git config --local user.email insightlabs38@gmail.com
echo "hooks installed; identity: $(git config user.name) <$(git config user.email)>"
