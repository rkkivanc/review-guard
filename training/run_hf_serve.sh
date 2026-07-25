#!/usr/bin/env bash
# Real model server (HF + PEFT). Stop the heuristic stub first (port 8000).
set -euo pipefail
cd "$(dirname "$0")"
if [[ ! -x .venv/bin/python ]]; then
  echo "Missing .venv — run:"
  echo "  /opt/homebrew/bin/python3.12 -m venv .venv && source .venv/bin/activate && pip install -r requirements.txt"
  exit 1
fi
export ADAPTERS_DIR="${ADAPTERS_DIR:-$(cd .. && pwd)/peft-adapters}"
export HOST="${HOST:-127.0.0.1}"
export PORT="${PORT:-8000}"
exec .venv/bin/python serve_hf.py
