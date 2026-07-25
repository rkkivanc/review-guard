# Human-feedback LoRA fine-tune + real inference

Train a PEFT LoRA adapter from Simulator/Dashboard **human label feedback**, serve it with Hugging Face (not the heuristic stub), then hot-swap via Admin.

Metal / MLC is optional later. For now: **train with HF → serve with `serve_hf.py` → Go backend unchanged**.

## Flow

```text
Label reviews in UI
        ↓
export_feedback.py  (store.json or Admin API)
        ↓
feedback.jsonl
        ↓
train_lora.py  (SmolLM2 + LoRA)
        ↓
peft-adapters/feedback-lora/
        ↓
Admin → Activate adapter
```

## 1) Collect labels

In the app: run Simulator classifications, open Dashboard, submit per-dimension feedback (correct / corrected label).

## 2) Export dataset

From API (admin JWT):

```bash
cd training
python export_feedback.py \
  --api http://localhost:8080 \
  --token "$ACCESS_TOKEN" \
  -o data/feedback.jsonl
```

Or from local memory store (no server):

```bash
python export_feedback.py \
  --store ../backend/data/store.json \
  -o data/feedback.jsonl
```

Also available as admin download:

`GET /admin/finetune/export?format=jsonl` (Bearer admin token)

## 3) Train LoRA

Use **Homebrew Python 3.12** (system `python3` is often 3.14 and breaks wheels):

```bash
# once
brew install python@3.12

cd training
/opt/homebrew/bin/python3.12 -m venv .venv
source .venv/bin/activate   # after this, `python` and `pip` work
pip install -r requirements.txt

python export_feedback.py --store ../backend/data/store.json -o data/feedback.jsonl

python train_lora.py \
  --data data/feedback.jsonl \
  --adapter-id feedback-lora \
  --epochs 3
```

Every new terminal: `cd training && source .venv/bin/activate` then run `python …`.

Smoke run (few steps):

```bash
python train_lora.py --data data/feedback.jsonl --adapter-id feedback-lora --max-steps 5
```

Default base model: `HuggingFaceTB/SmolLM2-360M-Instruct` (small, Mac-friendly).

## 4) Serve the real model (not the stub)

Stop `serve_local.py` if it holds `:8000`, then:

```bash
cd training
source .venv/bin/activate
./run_hf_serve.sh
# or: python serve_hf.py
```

`serve_hf.py` loads SmolLM2 on MPS/CPU, autoloads `feedback-lora` when present, and exposes the same OpenAI routes Go already uses (`/v1/chat/completions`, `/v1/adapters/activate`).

## 5) Activate in ReviewGuard

1. Backend: `MLC_LLM_URL=http://localhost:8000` and `PEFT_ADAPTERS_DIR=../peft-adapters`
2. Admin → Adapters → Activate `feedback-lora` (or rely on autoload)
3. Simulator classify — results come from the HF model + LoRA, not keyword heuristics

`GET http://127.0.0.1:8000/health` should show `"engine":"hf-peft"`.

## Notes

- Need **at least a handful** of labeled reviews; &lt;2 examples barely moves the adapter.
- Training writes Hugging Face PEFT weights under `peft-adapters/<id>/` plus `registry.json`.
- This LoRA is for the **HF causal LM** used in `train_lora.py`. Converting the same adapter into MLC runtime format is a separate Metal follow-up.
