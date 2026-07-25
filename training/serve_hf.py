#!/usr/bin/env python3
"""Real HF + PEFT OpenAI-compatible server (replaces heuristic stub).

Uses the same training venv (Python 3.12 + torch/peft). Loads SmolLM2 (or
adapter base) and hot-swaps LoRA folders under peft-adapters/.

  cd training && source .venv/bin/activate
  python serve_hf.py
  # http://127.0.0.1:8000

Point Go at it:
  MLC_LLM_URL=http://localhost:8000
"""

from __future__ import annotations

import json
import os
import re
import socketserver
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import torch
from peft import PeftModel
from transformers import AutoModelForCausalLM, AutoTokenizer

ROOT = Path(__file__).resolve().parents[1]
ADAPTERS_DIR = Path(os.environ.get("ADAPTERS_DIR", ROOT / "peft-adapters"))
DEFAULT_BASE = os.environ.get("HF_BASE_MODEL", "HuggingFaceTB/SmolLM2-360M-Instruct")
HOST = os.environ.get("HOST", "127.0.0.1")
PORT = int(os.environ.get("PORT", "8000"))
MAX_NEW = int(os.environ.get("HF_MAX_NEW_TOKENS", "512"))


class _Server(ThreadingHTTPServer):
    def server_bind(self) -> None:
        socketserver.TCPServer.server_bind(self)
        host, port = self.socket.getsockname()[:2]
        self.server_name = host
        self.server_port = port


class Engine:
    def __init__(self) -> None:
        self.mu = threading.Lock()
        self.device = "mps" if torch.backends.mps.is_available() else "cpu"
        self.base_id = DEFAULT_BASE
        self.active_adapter: str | None = None
        print(f"[hf-serve] loading base {self.base_id} on {self.device} …", flush=True)
        self.tokenizer = AutoTokenizer.from_pretrained(self.base_id, use_fast=True)
        if self.tokenizer.pad_token is None:
            self.tokenizer.pad_token = self.tokenizer.eos_token
        self.base = AutoModelForCausalLM.from_pretrained(self.base_id)
        self.base.to(self.device)
        self.base.eval()
        self.model = self.base
        # Autoload feedback-lora if present
        feedback = ADAPTERS_DIR / "feedback-lora"
        if (feedback / "adapter_config.json").is_file():
            print("[hf-serve] autoloading feedback-lora", flush=True)
            self._activate_unlocked("feedback-lora")

    def list_adapters(self) -> list[dict[str, Any]]:
        items: list[dict[str, Any]] = []
        reg = ADAPTERS_DIR / "registry.json"
        if reg.is_file():
            try:
                data = json.loads(reg.read_text(encoding="utf-8"))
                for row in data.get("adapters", []):
                    if isinstance(row, dict) and row.get("id"):
                        items.append(
                            {
                                "id": str(row["id"]),
                                "name": str(row.get("name") or row["id"]),
                                "path": str(row.get("path") or row["id"]),
                                "active": False,
                            }
                        )
            except (OSError, json.JSONDecodeError):
                pass
        for p in sorted(ADAPTERS_DIR.iterdir()) if ADAPTERS_DIR.is_dir() else []:
            if not p.is_dir() or p.name.startswith("."):
                continue
            if not (p / "adapter_config.json").is_file():
                continue
            if any(a["id"] == p.name for a in items):
                continue
            items.append({"id": p.name, "name": p.name, "path": p.name, "active": False})
        for a in items:
            a["active"] = a["id"] == self.active_adapter
        return items

    def activate(self, adapter_id: str | None) -> str | None:
        with self.mu:
            return self._activate_unlocked(adapter_id)

    def _activate_unlocked(self, adapter_id: str | None) -> str | None:
        aid = (adapter_id or "").strip() or None
        if aid is None:
            self.model = self.base
            self.active_adapter = None
            print("[hf-serve] adapter cleared (base only)", flush=True)
            return None
        path = ADAPTERS_DIR / aid
        if not (path / "adapter_config.json").is_file():
            raise FileNotFoundError(f"adapter not found or missing weights: {path}")
        # Always branch from clean base to avoid stacked adapters.
        self.model = PeftModel.from_pretrained(self.base, str(path))
        self.model.to(self.device)
        self.model.eval()
        self.active_adapter = aid
        # Prefer adapter's recorded base if different (informational).
        try:
            cfg = json.loads((path / "adapter_config.json").read_text(encoding="utf-8"))
            base = cfg.get("base_model_name_or_path")
            if base and base != self.base_id:
                print(f"[hf-serve] note: adapter base={base} (server base={self.base_id})", flush=True)
        except (OSError, json.JSONDecodeError):
            pass
        print(f"[hf-serve] active adapter={aid}", flush=True)
        return aid

    @torch.inference_mode()
    def complete(
        self,
        messages: list[dict[str, str]],
        temperature: float,
        max_tokens: int,
        top_p: float,
    ) -> str:
        with self.mu:
            model = self.model
            tokenizer = self.tokenizer
            device = self.device
        if hasattr(tokenizer, "apply_chat_template"):
            prompt = tokenizer.apply_chat_template(
                messages, tokenize=False, add_generation_prompt=True
            )
        else:
            prompt = "\n".join(f"{m['role']}: {m['content']}" for m in messages) + "\nassistant:"
        inputs = tokenizer(prompt, return_tensors="pt")
        inputs = {k: v.to(device) for k, v in inputs.items()}
        max_new = max(16, min(max_tokens or MAX_NEW, MAX_NEW))
        do_sample = temperature is not None and temperature > 0.01
        gen_kwargs: dict[str, Any] = {
            "max_new_tokens": max_new,
            "do_sample": do_sample,
            "pad_token_id": tokenizer.pad_token_id,
            "eos_token_id": tokenizer.eos_token_id,
        }
        if do_sample:
            gen_kwargs["temperature"] = max(0.05, float(temperature))
            if top_p and top_p > 0:
                gen_kwargs["top_p"] = float(top_p)
        out = model.generate(**inputs, **gen_kwargs)
        new_tokens = out[0][inputs["input_ids"].shape[-1] :]
        text = tokenizer.decode(new_tokens, skip_special_tokens=True).strip()
        classify = _is_classify_request(messages)
        prompt_blob = "\n".join(m.get("content", "") for m in messages)
        return _prefer_json_object(text, prompt_blob=prompt_blob, classify=classify)


_CLASSIFY_SYSTEM = (
    "You are a strict game-review analyst. Reply with ONLY one JSON object, no markdown, no prose. "
    "Keys must be exactly: consistency, authenticity, experience, usefulness. "
    'Each value MUST be an object like {"label":"aligned","confidence":0.82,"reason":"short"}. '
    "confidence is a single decimal number between 0 and 1 (example 0.82), never a range. "
    "Labels only: consistency aligned|mismatched; authenticity genuine|suspicious|bot; "
    "experience experience_based|speculative; usefulness useful|neutral|empty."
)

_CLASSIFY_FEWSHOT = (
    "Example valid reply:\n"
    '{"consistency":{"label":"aligned","confidence":0.84,"reason":"stars match tone"},'
    '"authenticity":{"label":"genuine","confidence":0.8,"reason":"specific play details"},'
    '"experience":{"label":"experience_based","confidence":0.86,"reason":"mentions hours played"},'
    '"usefulness":{"label":"useful","confidence":0.78,"reason":"actionable tips"}}'
)


def _is_classify_request(messages: list[dict[str, str]]) -> bool:
    blob = "\n".join(m.get("content", "") for m in messages)
    return "Star rating (1-10):" in blob or (
        "authenticity" in blob and "consistency" in blob and "usefulness" in blob
    )


def _ensure_classify_contract(messages: list[dict[str, str]]) -> list[dict[str, str]]:
    if not _is_classify_request(messages):
        return messages
    out = list(messages)
    sys = _CLASSIFY_SYSTEM + "\n" + _CLASSIFY_FEWSHOT
    if not out or out[0].get("role") != "system":
        out.insert(0, {"role": "system", "content": sys})
    else:
        out[0] = {"role": "system", "content": sys + "\n" + out[0].get("content", "")}
    return out


_DIMS = ("consistency", "authenticity", "experience", "usefulness")
_ALLOWED = {
    "consistency": {"aligned", "mismatched"},
    "authenticity": {"genuine", "suspicious", "bot"},
    "experience": {"experience_based", "speculative"},
    "usefulness": {"useful", "neutral", "empty"},
}


def _normalize_classification(obj: Any) -> dict[str, Any] | None:
    """Coerce model JSON into ReviewGuard nested label objects."""
    if not isinstance(obj, dict):
        return None
    out: dict[str, Any] = {}
    for dim in _DIMS:
        raw = obj.get(dim)
        label = ""
        conf = 0.7
        reason = "model output"
        if isinstance(raw, dict):
            label = str(raw.get("label") or "").strip().lower().replace(" ", "_")
            try:
                conf = float(raw.get("confidence", conf))
            except (TypeError, ValueError):
                conf = 0.7
            reason = str(raw.get("reason") or reason)
        elif isinstance(raw, str):
            label = raw.strip().lower().replace(" ", "_")
        if label not in _ALLOWED[dim]:
            # fuzzy contain
            for a in _ALLOWED[dim]:
                if a in label or label in a:
                    label = a
                    break
            else:
                label = next(iter(_ALLOWED[dim]))
        if conf > 1:
            conf = conf / 100.0
        conf = max(0.0, min(1.0, conf))
        # Schema-echo / missing conf often lands as 0 — use a neutral prior.
        if conf == 0.0:
            conf = 0.75
        out[dim] = {"label": label, "confidence": conf, "reason": reason[:200]}
    return out


def _repair_jsonish(s: str) -> str:
    """Fix common small-model JSON mistakes before json.loads."""
    s = s.strip()
    s = re.sub(r"^```(?:json)?\s*", "", s)
    s = re.sub(r"\s*```$", "", s)
    # confidence: 0.0-1.0 (schema echo) → 0.75, never leave as 0.0
    s = re.sub(
        r'("confidence"\s*:\s*)(\d+(?:\.\d+)?)\s*-\s*(\d+(?:\.\d+)?)',
        r"\g<1>0.75",
        s,
    )
    # trailing commas
    s = re.sub(r",\s*([}\]])", r"\1", s)
    # unquoted labels after "label":
    s = re.sub(
        r'("label"\s*:\s*)([A-Za-z][A-Za-z0-9_]*)\b',
        r'\1"\2"',
        s,
    )
    return s


def _heuristic_classification(prompt_blob: str) -> dict[str, Any]:
    """Always-valid fallback so Go never sees broken JSON."""
    lower = prompt_blob.lower()
    stars = 5
    m = re.search(r"star rating \(1-10\):\s*(\d+)", lower)
    if m:
        stars = int(m.group(1))
    speculative = any(w in lower for w in ("will be", "hype", "can't wait", "looks like"))
    experience = any(w in lower for w in ("played", "hours", "boss", "fps", "bug", "level"))
    botty = "buy now" in lower or lower.count("!") > 4
    negative = any(w in lower for w in ("bad", "trash", "awful", "hate", "crash"))
    positive = any(w in lower for w in ("great", "love", "amazing", "fun", "excellent"))
    if stars >= 7 and negative and not positive:
        consistency = "mismatched"
    elif stars <= 4 and positive and not negative:
        consistency = "mismatched"
    else:
        consistency = "aligned"
    authenticity = "bot" if botty else "genuine"
    exp = "speculative" if speculative and not experience else "experience_based"
    useful = "useful" if experience else ("empty" if len(lower) < 80 else "neutral")
    return {
        "consistency": {"label": consistency, "confidence": 0.72, "reason": "fallback heuristic"},
        "authenticity": {"label": authenticity, "confidence": 0.7, "reason": "fallback heuristic"},
        "experience": {"label": exp, "confidence": 0.71, "reason": "fallback heuristic"},
        "usefulness": {"label": useful, "confidence": 0.69, "reason": "fallback heuristic"},
    }


def _prefer_json_object(text: str, prompt_blob: str = "", classify: bool = False) -> str:
    """Extract + normalize classification JSON; never return broken JSON for classify."""
    s = _repair_jsonish(text)
    start, end = s.find("{"), s.rfind("}")
    if start >= 0 and end > start:
        candidate = _repair_jsonish(s[start : end + 1])
        try:
            parsed = json.loads(candidate)
            norm = _normalize_classification(parsed)
            if norm is not None:
                return json.dumps(norm, ensure_ascii=True)
            if not classify:
                return candidate
        except json.JSONDecodeError:
            pass
    if classify:
        return json.dumps(_heuristic_classification(prompt_blob + "\n" + text), ensure_ascii=True)
    return text


ENGINE: Engine | None = None


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt: str, *args: Any) -> None:
        print(f"[hf-serve] {self.address_string()} {fmt % args}", flush=True)

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
        return json.loads(raw.decode("utf-8")) if raw else {}

    def do_OPTIONS(self) -> None:  # noqa: N802
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")
        self.end_headers()

    def do_GET(self) -> None:  # noqa: N802
        assert ENGINE is not None
        path = urlparse(self.path).path
        if path == "/health":
            self._send(
                200,
                {
                    "status": "ok",
                    "engine": "hf-peft",
                    "model": ENGINE.base_id,
                    "active_adapter": ENGINE.active_adapter,
                    "device": ENGINE.device,
                },
            )
            return
        if path == "/v1/models":
            mid = ENGINE.active_adapter or ENGINE.base_id
            self._send(
                200,
                {
                    "object": "list",
                    "data": [{"id": mid, "object": "model", "owned_by": "reviewguard-hf"}],
                },
            )
            return
        if path == "/v1/adapters":
            self._send(200, {"object": "list", "data": ENGINE.list_adapters()})
            return
        self._send(404, {"detail": "not found"})

    def do_POST(self) -> None:  # noqa: N802
        assert ENGINE is not None
        path = urlparse(self.path).path
        try:
            body = self._read_json()
        except json.JSONDecodeError:
            self._send(400, {"detail": "invalid JSON"})
            return

        if path == "/v1/adapters/activate":
            aid = body.get("adapter_id")
            try:
                active = ENGINE.activate(None if aid in (None, "") else str(aid))
            except FileNotFoundError as e:
                self._send(404, {"detail": str(e)})
                return
            except Exception as e:  # noqa: BLE001
                self._send(500, {"detail": str(e)})
                return
            self._send(200, {"ok": True, "active_adapter": active})
            return

        if path != "/v1/chat/completions":
            self._send(404, {"detail": "not found"})
            return

        messages = body.get("messages") or []
        if not messages:
            self._send(400, {"detail": "messages required"})
            return
        # Optional per-request adapter override
        if "adapter_id" in body:
            try:
                ENGINE.activate(body.get("adapter_id") or None)
            except Exception as e:  # noqa: BLE001
                self._send(400, {"detail": f"adapter: {e}"})
                return

        norm = [{"role": str(m.get("role", "user")), "content": str(m.get("content", ""))} for m in messages]
        norm = _ensure_classify_contract(norm)
        temperature = float(body.get("temperature") or 0.7)
        top_p = float(body.get("top_p") or 1.0)
        max_tokens = int(body.get("max_tokens") or MAX_NEW)
        # Classification needs lower temperature for valid JSON labels.
        if any("consistency" in (m.get("content") or "") and "authenticity" in (m.get("content") or "") for m in norm):
            temperature = min(temperature, 0.3)
        started = time.time()
        try:
            content = ENGINE.complete(norm, temperature, max_tokens, top_p)
        except Exception as e:  # noqa: BLE001
            self._send(500, {"detail": f"generation failed: {e}"})
            return
        mid = ENGINE.active_adapter or ENGINE.base_id
        self._send(
            200,
            {
                "id": f"chatcmpl-hf-{int(time.time() * 1000)}",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": body.get("model") or mid,
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": content},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": 0,
                    "completion_tokens": 0,
                    "total_tokens": 0,
                },
                "adapter_id": ENGINE.active_adapter,
                "latency_ms": int((time.time() - started) * 1000),
            },
        )


def main() -> None:
    global ENGINE
    ENGINE = Engine()
    server = _Server((HOST, PORT), Handler)
    print(f"[hf-serve] listening on http://{HOST}:{PORT} adapters={ADAPTERS_DIR}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[hf-serve] stopped", flush=True)


if __name__ == "__main__":
    main()
