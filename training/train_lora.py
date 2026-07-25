#!/usr/bin/env python3
"""LoRA fine-tune on human-feedback JSONL, then register under peft-adapters/.

Example (Mac MPS / CPU — small base model):
  python export_feedback.py --store ../backend/data/store.json -o data/feedback.jsonl
  python train_lora.py --data data/feedback.jsonl --adapter-id feedback-lora

Requires: pip install -r requirements.txt
Metal MLC serve is NOT required for this training step.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

import torch
from datasets import Dataset
from peft import LoraConfig, TaskType, get_peft_model
from transformers import (
    AutoModelForCausalLM,
    AutoTokenizer,
    DataCollatorForLanguageModeling,
    Trainer,
    TrainingArguments,
)

DEFAULT_BASE = "HuggingFaceTB/SmolLM2-360M-Instruct"


def load_jsonl(path: Path) -> list[dict]:
    rows = []
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        rows.append(json.loads(line))
    return rows


def format_example(tokenizer, messages: list[dict]) -> str:
    if hasattr(tokenizer, "apply_chat_template"):
        try:
            return tokenizer.apply_chat_template(
                messages, tokenize=False, add_generation_prompt=False
            )
        except Exception:
            pass
    parts = []
    for m in messages:
        parts.append(f"<|{m['role']}|>\n{m['content']}\n")
    return "".join(parts)


def register_adapter(adapters_dir: Path, adapter_id: str, name: str, description: str) -> None:
    adapters_dir.mkdir(parents=True, exist_ok=True)
    reg_path = adapters_dir / "registry.json"
    if reg_path.is_file():
        reg = json.loads(reg_path.read_text(encoding="utf-8"))
    else:
        reg = {"adapters": []}
    adapters = [a for a in reg.get("adapters", []) if a.get("id") != adapter_id]
    adapters.append(
        {
            "id": adapter_id,
            "name": name,
            "path": adapter_id,
            "description": description,
        }
    )
    reg["adapters"] = adapters
    reg_path.write_text(json.dumps(reg, indent=2) + "\n", encoding="utf-8")
    print(f"registered adapter {adapter_id} in {reg_path}")


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--data", type=Path, required=True, help="feedback JSONL from export_feedback.py")
    p.add_argument("--base-model", default=DEFAULT_BASE)
    p.add_argument("--adapter-id", default="feedback-lora")
    p.add_argument(
        "--adapters-dir",
        type=Path,
        default=Path(__file__).resolve().parents[1] / "peft-adapters",
    )
    p.add_argument("--epochs", type=float, default=3.0)
    p.add_argument("--lr", type=float, default=2e-4)
    p.add_argument("--batch-size", type=int, default=1)
    p.add_argument("--max-steps", type=int, default=-1, help="Optional cap for smoke runs")
    p.add_argument("--max-length", type=int, default=1024)
    args = p.parse_args()

    rows = load_jsonl(args.data)
    if not rows:
        print("empty dataset", file=sys.stderr)
        return 1
    if len(rows) < 2:
        print("warning: <2 examples — LoRA will barely learn; label more reviews", file=sys.stderr)

    out_dir = args.adapters_dir / args.adapter_id
    out_dir.mkdir(parents=True, exist_ok=True)

    print(f"loading base model {args.base_model} …")
    tokenizer = AutoTokenizer.from_pretrained(args.base_model, use_fast=True)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    model = AutoModelForCausalLM.from_pretrained(args.base_model)
    lora = LoraConfig(
        task_type=TaskType.CAUSAL_LM,
        r=8,
        lora_alpha=16,
        lora_dropout=0.05,
        target_modules=["q_proj", "v_proj", "k_proj", "o_proj"],
    )
    model = get_peft_model(model, lora)
    model.print_trainable_parameters()

    texts = [format_example(tokenizer, r["messages"]) for r in rows]

    def tokenize(batch):
        return tokenizer(
            batch["text"],
            truncation=True,
            max_length=args.max_length,
            padding=False,
        )

    ds = Dataset.from_dict({"text": texts}).map(tokenize, batched=True, remove_columns=["text"])

    use_mps = torch.backends.mps.is_available()
    targs = TrainingArguments(
        output_dir=str(out_dir / "checkpoints"),
        num_train_epochs=args.epochs,
        per_device_train_batch_size=args.batch_size,
        gradient_accumulation_steps=4,
        learning_rate=args.lr,
        logging_steps=1,
        save_strategy="no",
        report_to=[],
        max_steps=args.max_steps if args.max_steps > 0 else -1,
        fp16=False,
        bf16=False,
        use_cpu=not use_mps,
        remove_unused_columns=False,
    )

    collator = DataCollatorForLanguageModeling(tokenizer=tokenizer, mlm=False)
    trainer = Trainer(
        model=model,
        args=targs,
        train_dataset=ds,
        data_collator=collator,
    )
    trainer.train()

    model.save_pretrained(out_dir)
    tokenizer.save_pretrained(out_dir)
    meta = {
        "adapter_id": args.adapter_id,
        "base_model": args.base_model,
        "examples": len(rows),
        "trained_at": int(time.time()),
        "source": "human-feedback",
    }
    (out_dir / "reviewguard_meta.json").write_text(json.dumps(meta, indent=2) + "\n", encoding="utf-8")

    register_adapter(
        args.adapters_dir,
        args.adapter_id,
        name="Human feedback LoRA",
        description=f"LoRA from {len(rows)} human-labeled reviews (base={args.base_model})",
    )
    print(f"saved adapter → {out_dir}")
    print("Next: Admin → Adapters → Activate 'feedback-lora' (restart API if registry was empty).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
