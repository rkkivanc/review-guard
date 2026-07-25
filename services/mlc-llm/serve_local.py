#!/usr/bin/env python3
"""Stdlib-only OpenAI-compatible stub for host `go run` (no pip / Docker).

  python3 serve_local.py
  # listens on http://127.0.0.1:8000
"""

from __future__ import annotations

import json
import os
import random
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import urlparse
import socketserver


class _Server(ThreadingHTTPServer):
    # Avoid socket.getfqdn() hang on some macOS DNS setups during bind.
    def server_bind(self) -> None:
        socketserver.TCPServer.server_bind(self)
        host, port = self.socket.getsockname()[:2]
        self.server_name = host
        self.server_port = port


MODEL_ID = os.environ.get("MODEL_ID", "gemma-2-2b-it-q4f16_1-MLC")
ADAPTERS_DIR = Path(os.environ.get("ADAPTERS_DIR", str(Path(__file__).resolve().parents[2] / "peft-adapters")))
HOST = os.environ.get("HOST", "127.0.0.1")
PORT = int(os.environ.get("PORT", "8000"))

_lock = threading.Lock()
_active_adapter: str | None = None


def _list_adapters() -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    if ADAPTERS_DIR.is_dir():
        registry = ADAPTERS_DIR / "registry.json"
        if registry.is_file():
            try:
                data = json.loads(registry.read_text(encoding="utf-8"))
                for row in data.get("adapters", []):
                    if isinstance(row, dict) and row.get("id"):
                        items.append(
                            {
                                "id": str(row["id"]),
                                "name": str(row.get("name") or row["id"]),
                                "path": str(row.get("path") or ""),
                                "active": False,
                            }
                        )
            except (OSError, json.JSONDecodeError):
                pass
        for p in sorted(ADAPTERS_DIR.iterdir()):
            if p.name.startswith(".") or p.name == "registry.json":
                continue
            aid = p.stem if p.is_file() else p.name
            if any(a["id"] == aid for a in items):
                continue
            items.append({"id": aid, "name": aid, "path": str(p), "active": False})
    with _lock:
        active = _active_adapter
    for a in items:
        a["active"] = a["id"] == active
    return items


def _extract_review(prompt: str) -> tuple[str, int, str]:
    game, stars, text = "Unknown", 5, ""
    m_game = re.search(r"Game:\s*(.+)", prompt)
    if m_game:
        game = m_game.group(1).strip()[:120]
    m_stars = re.search(r"Star rating \(1-10\):\s*(\d+)", prompt)
    if m_stars:
        stars = int(m_stars.group(1))
    m_rev = re.search(r'Review:\s*"([\s\S]*?)"\s*(?:\n|</untrusted_input>)', prompt)
    if m_rev:
        text = m_rev.group(1).strip()
    return game, stars, text


def _adapter_bias(adapter_id: str | None) -> dict[str, float]:
    if not adapter_id:
        return {"auth": 0.0, "exp": 0.0, "strict": 0.0}
    key = adapter_id.lower()
    if "strict" in key or "security" in key:
        return {"auth": -0.08, "exp": 0.05, "strict": 0.12}
    if "wiki" in key or "deepkwiki" in key or "knowledge" in key:
        return {"auth": 0.05, "exp": 0.1, "strict": -0.05}
    return {"auth": 0.03, "exp": 0.03, "strict": 0.0}


def _classify(game: str, stars: int, text: str, temperature: float, adapter_id: str | None) -> dict[str, Any]:
    lower = text.lower()
    speculative = any(w in lower for w in ("will be", "can't wait", "hype", "looks like", "gonna be", "heard that"))
    experience = any(w in lower for w in ("played", "boss", "bug", "fps", "level", "quest", "hours", "mechanics"))
    negative = any(w in lower for w in ("bad", "awful", "trash", "boring", "crash", "hate"))
    positive = any(w in lower for w in ("great", "love", "amazing", "fun", "excellent", "best"))
    botty = len(text) < 40 or text.count("!") > 4
    bias = _adapter_bias(adapter_id)

    if stars >= 7 and negative and not positive:
        consistency = "mismatched"
    elif stars <= 4 and positive and not negative:
        consistency = "mismatched"
    else:
        consistency = "aligned"

    authenticity = "bot" if botty else ("suspicious" if "buy now" in lower else "genuine")
    exp_label = "speculative" if speculative and not experience else "experience_based"
    if len(text) < 30:
        useful = "empty"
    elif experience or ("tip" in lower or "recommend" in lower):
        useful = "useful"
    else:
        useful = "neutral"

    jitter = 0.05 * float(temperature)

    def conf(base: float) -> float:
        return round(min(0.99, max(0.35, base + random.uniform(-jitter, jitter))), 2)

    return {
        "consistency": {
            "label": consistency,
            "confidence": conf(0.82 + bias["strict"]),
            "reason": f"Stars={stars} vs tone for {game[:40]} (adapter={adapter_id or 'none'})",
        },
        "authenticity": {
            "label": authenticity,
            "confidence": conf(0.78 + bias["auth"]),
            "reason": "Heuristic authenticity from length/phrasing",
        },
        "experience": {
            "label": exp_label,
            "confidence": conf(0.8 + bias["exp"]),
            "reason": "Checked for play specifics vs hype language",
        },
        "usefulness": {
            "label": useful,
            "confidence": conf(0.75),
            "reason": "Buyer-actionable detail estimate",
        },
    }


def _extract_user_query(prompt: str) -> str:
    """Pull the real user question out of the DeepKwiki package prompt."""
    m = re.search(r"## User query\n([\s\S]*?)(?:\n## |\nRespond in Markdown|\Z)", prompt)
    if m:
        return m.group(1).strip()
    # Fallback: last non-empty line that isn't an instruction header.
    lines = [ln.strip() for ln in prompt.splitlines() if ln.strip()]
    for ln in reversed(lines):
        if ln.startswith("#") or ln.startswith("Respond in") or ln.startswith("- "):
            continue
        if ln.startswith("Product:") or ln.startswith("Roles:") or ln.startswith("Transport:"):
            continue
        return ln
    return prompt.strip()[:200] or "(empty)"


def _answer_for_query(q: str, adapter_id: str | None) -> str:
    """Stub knowledge answers keyed off the user question (not the full prompt)."""
    lower = q.lower()
    adapter = adapter_id or "none"

    if "deepkwiki" in lower or ("what is" in lower and "wiki" in lower):
        return f"""# What is DeepKwiki?

DeepKwiki is ReviewGuard’s **knowledge query** surface. You ask a free-form question; the app does **not** run the 3-run review classifier.

## How it works

| Step | What happens |
|------|----------------|
| 1 | Frontend WebMCP packs your question + static product specs |
| 2 | Go backend applies the live system prompt + active PEFT adapter |
| 3 | Local MLC-LLM (or this stub) returns Markdown |
| 4 | UI renders a Rich Result (text, tables, charts) |

## Vs Simulator

| | DeepKwiki | Simulator |
|--|-----------|-----------|
| Input | Natural-language question | Game + stars + review text |
| Output | Explainer / knowledge Markdown | Trust score + A–F grade |
| Path | `deepkwiki.search` MCP tool | `POST /reviews` |

**Active adapter:** `{adapter}`

## Chart data

```chart
{{"type":"bar","title":"Trust weight mix","labels":["authenticity","experience","consistency","usefulness"],"values":[30,30,25,15]}}
```
"""

    if "admin" in lower:
        return f"""# Admin panel

The Admin view is the LLM control plane (admin role only).

| Module | Purpose |
|--------|---------|
| Adapters | Register / activate / remove PEFT LoRA adapters (hot-swap) |
| System prompt | Change the model character without restart |
| Context limits | `max_tokens`, temperature, top-p |
| Log monitor | Recent MCP queries and latency |

**Active adapter:** `{adapter}`
"""

    if any(k in lower for k in ("authenticity", "experience", "consistency", "usefulness", "dimension")):
        return f"""# Classification dimensions

ReviewGuard scores reviews on four dimensions:

| Dimension | Labels |
|-----------|--------|
| consistency | aligned, mismatched |
| authenticity | genuine, suspicious, bot |
| experience | experience_based, speculative |
| usefulness | useful, neutral, empty |

Weights (trust mix): authenticity 30%, experience 30%, consistency 25%, usefulness 15%.

**Active adapter:** `{adapter}`

```chart
{{"type":"bar","title":"Trust weight mix","labels":["authenticity","experience","consistency","usefulness"],"values":[30,30,25,15]}}
```
"""

    if "peft" in lower or "adapter" in lower or "lora" in lower:
        return f"""# PEFT adapters

PEFT (LoRA/QLoRA) adapters nudge model behavior without reloading full weights.

1. Register an adapter in **Admin → Adapters**
2. Click **Activate** (hot-swap; no container restart)
3. Re-run DeepKwiki or Simulator — requests carry `adapter_id`

**Currently active:** `{adapter}`
"""

    # Generic fallback: still answer the question, don't dump the prompt.
    return f"""# DeepKwiki answer

**Your question:** {q}

*(Local stub)* I don’t have a dedicated article for this yet. In ReviewGuard terms:

- **DeepKwiki** = knowledge Q&A over MCP  
- **Simulator** = review classification + trust score  
- **Admin** = hot-swap prompt / PEFT / sampling  

Try asking: “What is DeepKwiki?”, “What is the Admin panel?”, or “Explain authenticity vs experience”.

**Active adapter:** `{adapter}`
"""


def _deepkwiki_answer(prompt: str, adapter_id: str | None) -> str:
    q = _extract_user_query(prompt)
    return _answer_for_query(q, adapter_id)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt: str, *args: Any) -> None:
        print(f"[mlc-stub] {self.address_string()} {fmt % args}")

    def _send(self, code: int, payload: Any) -> None:
        raw = json.dumps(payload).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(raw)

    def _read_json(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length") or "0")
        raw = self.rfile.read(length) if length else b"{}"
        if not raw:
            return {}
        return json.loads(raw.decode("utf-8"))

    def do_OPTIONS(self) -> None:  # noqa: N802
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")
        self.end_headers()

    def do_GET(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        if path == "/health":
            with _lock:
                active = _active_adapter
            self._send(200, {"status": "ok", "model": MODEL_ID, "active_adapter": active})
            return
        if path == "/v1/models":
            self._send(
                200,
                {
                    "object": "list",
                    "data": [{"id": MODEL_ID, "object": "model", "owned_by": "reviewguard-local"}],
                },
            )
            return
        if path == "/v1/adapters":
            self._send(200, {"object": "list", "data": _list_adapters()})
            return
        self._send(404, {"detail": "not found"})

    def do_POST(self) -> None:  # noqa: N802
        global _active_adapter
        path = urlparse(self.path).path
        try:
            body = self._read_json()
        except json.JSONDecodeError:
            self._send(400, {"detail": "invalid JSON"})
            return

        if path == "/v1/adapters/activate":
            aid = str(body.get("adapter_id") or "").strip() or None
            with _lock:
                _active_adapter = aid
            self._send(200, {"ok": True, "active_adapter": aid})
            return

        if path != "/v1/chat/completions":
            self._send(404, {"detail": "not found"})
            return

        messages = body.get("messages") or []
        if not messages:
            self._send(400, {"detail": "messages required"})
            return

        with _lock:
            adapter = body.get("adapter_id")
            if adapter is None:
                adapter = _active_adapter

        user_parts = [m.get("content", "") for m in messages if m.get("role") == "user"]
        prompt = "\n".join(str(p) for p in user_parts if p)
        if not prompt.strip():
            self._send(400, {"detail": "empty user prompt"})
            return

        temperature = float(body.get("temperature") or 0.7)
        if "Star rating (1-10):" not in prompt and "<untrusted_input>" not in prompt:
            content = _deepkwiki_answer(prompt, adapter)
        else:
            game, stars, text = _extract_review(prompt)
            content = json.dumps(_classify(game, stars, text, temperature, adapter), ensure_ascii=True)

        self._send(
            200,
            {
                "id": f"chatcmpl-local-{int(time.time() * 1000)}",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": body.get("model") or MODEL_ID,
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": content},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": max(1, len(prompt) // 4),
                    "completion_tokens": max(1, len(content) // 4),
                    "total_tokens": max(2, (len(prompt) + len(content)) // 4),
                },
                "adapter_id": adapter,
            },
        )


def main() -> None:
    server = _Server((HOST, PORT), Handler)
    print(f"reviewguard mlc stub listening on http://{HOST}:{PORT} (adapters={ADAPTERS_DIR})", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nstopped")


if __name__ == "__main__":
    main()
