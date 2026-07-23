"""OpenAI-compatible chat completions service for ReviewGuard (MLC LLM contract).

Serves POST /v1/chat/completions and returns classification JSON matching
the ReviewGuard prompt contract. Suitable for local Docker; replace the image
with a full MLC-LLM runtime when GPU inference is available.
"""

from __future__ import annotations

import json
import random
import re
import time
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

app = FastAPI(title="reviewguard-mlc-llm", version="0.1.0")

MODEL_ID = "gemma-2-2b-it-q4f16_1-MLC"


class ChatMessage(BaseModel):
    role: str
    content: str


class ChatCompletionRequest(BaseModel):
    model: str | None = None
    messages: list[ChatMessage]
    temperature: float = 0.7
    max_tokens: int = 512


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


def _classify(game: str, stars: int, text: str, temperature: float) -> dict[str, Any]:
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

    # Consistency: stars vs sentiment
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
            "confidence": conf(0.82),
            "reason": f"Stars={stars} vs tone for {game[:40]}",
        },
        "authenticity": {
            "label": authenticity,
            "confidence": conf(0.78),
            "reason": "Heuristic authenticity from length/phrasing",
        },
        "experience": {
            "label": exp_label,
            "confidence": conf(0.8),
            "reason": "Checked for play specifics vs hype language",
        },
        "usefulness": {
            "label": useful,
            "confidence": conf(0.75),
            "reason": "Buyer-actionable detail estimate",
        },
    }


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok", "model": MODEL_ID}


@app.get("/v1/models")
def models() -> dict[str, Any]:
    return {
        "object": "list",
        "data": [{"id": MODEL_ID, "object": "model", "owned_by": "reviewguard-local"}],
    }


@app.post("/v1/chat/completions")
def chat_completions(req: ChatCompletionRequest) -> dict[str, Any]:
    if not req.messages:
        raise HTTPException(status_code=400, detail="messages required")
    prompt = "\n".join(m.content for m in req.messages if m.role == "user")
    if not prompt.strip():
        raise HTTPException(status_code=400, detail="empty user prompt")

    game, stars, text = _extract_review(prompt)
    payload = _classify(game, stars, text, req.temperature)
    content = json.dumps(payload, ensure_ascii=True)

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
    }
