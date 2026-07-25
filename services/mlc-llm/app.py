"""OpenAI-compatible chat completions service for ReviewGuard (MLC LLM contract).

Serves POST /v1/chat/completions and adapter hot-swap endpoints matching the
local MLC contract. Suitable for CPU Docker; use compose profile `gpu` for
the real MLC-LLM runtime when NVIDIA hardware is available.
"""

from __future__ import annotations

import json
import os
import random
import re
import threading
import time
from pathlib import Path
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

app = FastAPI(title="reviewguard-mlc-llm", version="0.2.0")

MODEL_ID = os.environ.get("MODEL_ID", "gemma-2-2b-it-q4f16_1-MLC")
ADAPTERS_DIR = Path(os.environ.get("ADAPTERS_DIR", "/adapters"))

_lock = threading.Lock()
_active_adapter: str | None = None
_system_prompt_hint: str = ""


class ChatMessage(BaseModel):
    role: str
    content: str


class ChatCompletionRequest(BaseModel):
    model: str | None = None
    messages: list[ChatMessage]
    temperature: float = 0.7
    top_p: float = 1.0
    max_tokens: int = 512
    adapter_id: str | None = None


class ActivateAdapterRequest(BaseModel):
    adapter_id: str | None = Field(default=None, description="Adapter id or null to clear")


def _list_adapters() -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    if not ADAPTERS_DIR.is_dir():
        return items
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
    game = "Unknown"
    stars = 5
    text = ""
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
    """Stub PEFT: adapter id nudges heuristic confidences / labels."""
    if not adapter_id:
        return {"auth": 0.0, "exp": 0.0, "strict": 0.0}
    key = adapter_id.lower()
    if "strict" in key or "security" in key:
        return {"auth": -0.08, "exp": 0.05, "strict": 0.12}
    if "wiki" in key or "deepkwiki" in key or "knowledge" in key:
        return {"auth": 0.05, "exp": 0.1, "strict": -0.05}
    if "code" in key:
        return {"auth": 0.02, "exp": 0.08, "strict": 0.05}
    return {"auth": 0.03, "exp": 0.03, "strict": 0.0}


def _classify(
    game: str,
    stars: int,
    text: str,
    temperature: float,
    adapter_id: str | None,
    system_hint: str,
) -> dict[str, Any]:
    lower = text.lower()
    speculative = any(
        w in lower
        for w in ("will be", "can't wait", "hype", "looks like", "gonna be", "heard that")
    )
    experience = any(
        w in lower
        for w in ("played", "boss", "bug", "fps", "level", "quest", "hours", "mechanics")
    )
    negative = any(w in lower for w in ("bad", "awful", "trash", "boring", "crash", "hate"))
    positive = any(w in lower for w in ("great", "love", "amazing", "fun", "excellent", "best"))
    botty = len(text) < 40 or text.count("!") > 4
    bias = _adapter_bias(adapter_id)
    if "skeptic" in system_hint.lower() or bias["strict"] > 0.1:
        botty = botty or ("buy" in lower)

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

    jitter = 0.05 * temperature
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
    m = re.search(r"## User query\n([\s\S]*?)(?:\n## |\nRespond in Markdown|\Z)", prompt)
    if m:
        return m.group(1).strip()
    lines = [ln.strip() for ln in prompt.splitlines() if ln.strip()]
    for ln in reversed(lines):
        if ln.startswith("#") or ln.startswith("Respond in") or ln.startswith("- "):
            continue
        if ln.startswith("Product:") or ln.startswith("Roles:") or ln.startswith("Transport:"):
            continue
        return ln
    return prompt.strip()[:200] or "(empty)"


def _answer_for_query(q: str, adapter_id: str | None) -> str:
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

| Dimension | Labels |
|-----------|--------|
| consistency | aligned, mismatched |
| authenticity | genuine, suspicious, bot |
| experience | experience_based, speculative |
| usefulness | useful, neutral, empty |

Weights: authenticity 30%, experience 30%, consistency 25%, usefulness 15%.

**Active adapter:** `{adapter}`

```chart
{{"type":"bar","title":"Trust weight mix","labels":["authenticity","experience","consistency","usefulness"],"values":[30,30,25,15]}}
```
"""

    if "peft" in lower or "adapter" in lower or "lora" in lower:
        return f"""# PEFT adapters

1. Register an adapter in **Admin → Adapters**
2. Click **Activate** (hot-swap)
3. Re-run DeepKwiki or Simulator

**Currently active:** `{adapter}`
"""

    return f"""# DeepKwiki answer

**Your question:** {q}

*(Local stub)* No dedicated article for this yet. Try: “What is DeepKwiki?”, “What is the Admin panel?”, or “Explain authenticity vs experience”.

**Active adapter:** `{adapter}`
"""


def _deepkwiki_answer(prompt: str, adapter_id: str | None) -> str:
    """Markdown rich result for DeepKwiki — answers the user question, not the wrapper prompt."""
    return _answer_for_query(_extract_user_query(prompt), adapter_id)


@app.get("/health")
def health() -> dict[str, Any]:
    with _lock:
        active = _active_adapter
    return {"status": "ok", "model": MODEL_ID, "active_adapter": active}


@app.get("/v1/models")
def models() -> dict[str, Any]:
    return {
        "object": "list",
        "data": [{"id": MODEL_ID, "object": "model", "owned_by": "reviewguard-local"}],
    }


@app.get("/v1/adapters")
def adapters() -> dict[str, Any]:
    return {"object": "list", "data": _list_adapters()}


@app.post("/v1/adapters/activate")
def activate_adapter(req: ActivateAdapterRequest) -> dict[str, Any]:
    global _active_adapter
    aid = (req.adapter_id or "").strip() or None
    if aid:
        known = {a["id"] for a in _list_adapters()}
        # Allow activating ids even if file not yet present (admin registry push).
        if known and aid not in known:
            # Still accept — backend registry is source of truth for uploads.
            pass
    with _lock:
        _active_adapter = aid
    return {"ok": True, "active_adapter": aid}


@app.post("/v1/chat/completions")
def chat_completions(req: ChatCompletionRequest) -> dict[str, Any]:
    if not req.messages:
        raise HTTPException(status_code=400, detail="messages required")

    with _lock:
        adapter = req.adapter_id if req.adapter_id is not None else _active_adapter
        system_hint = _system_prompt_hint
        for m in req.messages:
            if m.role == "system" and m.content:
                system_hint = m.content
                break

    user_parts = [m.content for m in req.messages if m.role == "user" and m.content]
    prompt = "\n".join(user_parts)
    if not prompt.strip():
        raise HTTPException(status_code=400, detail="empty user prompt")

    # DeepKwiki / free-form: no review extraction markers → markdown rich answer.
    if "Star rating (1-10):" not in prompt and "<untrusted_input>" not in prompt:
        content = _deepkwiki_answer(prompt, adapter)
    else:
        game, stars, text = _extract_review(prompt)
        payload = _classify(game, stars, text, req.temperature, adapter, system_hint)
        content = json.dumps(payload, ensure_ascii=True)

    # Approximate sampling cost (unused except for realistic usage stats).
    _ = max(0.0, min(1.0, req.top_p))
    _ = max(1, req.max_tokens)

    return {
        "id": f"chatcmpl-local-{int(time.time() * 1000)}",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": req.model or MODEL_ID,
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
    }
