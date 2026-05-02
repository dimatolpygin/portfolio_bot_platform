#!/usr/bin/env bash
set -euo pipefail

test -f bot.yaml
test -f schemas/bot.schema.json
test -f tests/validate_manifest.py
grep -q "^slug: " bot.yaml
grep -q "^module: " bot.yaml
