"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  adminApi,
  type AdapterMeta,
  type LLMRuntimeConfig,
  type QueryLogEntry,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";

type Tab = "adapters" | "prompt" | "limits" | "logs" | "finetune";

export function AdminView() {
  const { accessToken } = useAuth();
  const [tab, setTab] = useState<Tab>("adapters");
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const [adapters, setAdapters] = useState<AdapterMeta[]>([]);
  const [config, setConfig] = useState<LLMRuntimeConfig | null>(null);
  const [logs, setLogs] = useState<QueryLogEntry[]>([]);
  const [finetuneCount, setFinetuneCount] = useState<number | null>(null);

  const [newId, setNewId] = useState("");
  const [newName, setNewName] = useState("");
  const [newDesc, setNewDesc] = useState("");

  const refresh = useCallback(async () => {
    if (!accessToken) return;
    setError(null);
    try {
      const [a, c, l, ft] = await Promise.all([
        adminApi.listAdapters(accessToken),
        adminApi.getLLMConfig(accessToken),
        adminApi.listLogs(accessToken, 40),
        adminApi.exportFinetune(accessToken),
      ]);
      setAdapters(a.adapters);
      setConfig(c);
      setLogs(l.logs);
      setFinetuneCount(ft.count);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load admin data");
    }
  }, [accessToken]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function activate(id: string) {
    if (!accessToken) return;
    setBusy(true);
    setNotice(null);
    try {
      const c = await adminApi.activateAdapter(accessToken, id);
      setConfig(c);
      setNotice(`Active adapter: ${c.active_adapter || "(none)"}`);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Activate failed");
    } finally {
      setBusy(false);
    }
  }

  async function removeAdapter(id: string) {
    if (!accessToken) return;
    setBusy(true);
    try {
      await adminApi.removeAdapter(accessToken, id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Remove failed");
    } finally {
      setBusy(false);
    }
  }

  async function onAddAdapter(e: FormEvent) {
    e.preventDefault();
    if (!accessToken) return;
    setBusy(true);
    setError(null);
    try {
      await adminApi.upsertAdapter(accessToken, {
        id: newId.trim(),
        name: newName.trim() || newId.trim(),
        path: newId.trim(),
        description: newDesc.trim(),
      });
      setNewId("");
      setNewName("");
      setNewDesc("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Upsert failed");
    } finally {
      setBusy(false);
    }
  }

  async function saveConfig(patch: Partial<LLMRuntimeConfig>) {
    if (!accessToken) return;
    setBusy(true);
    setNotice(null);
    try {
      const c = await adminApi.patchLLMConfig(accessToken, patch);
      setConfig(c);
      setNotice("LLM config hot-swapped (no restart).");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Update failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <h1>Admin · LLM control</h1>
      <p className="muted">
        Hot-swap PEFT adapters, system prompt, and sampling limits. Changes apply to the next
        WebMCP / classification call.
      </p>

      <div className="subnav" role="tablist" aria-label="Admin modules">
        {(
          [
            ["adapters", "Adapters"],
            ["prompt", "System prompt"],
            ["limits", "Context limits"],
            ["logs", "Log monitor"],
            ["finetune", "Finetune"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            className={tab === id ? "active" : ""}
            onClick={() => setTab(id)}
          >
            {label}
          </button>
        ))}
      </div>

      {error ? <div className="error-box">{error}</div> : null}
      {notice ? <div className="notice-box">{notice}</div> : null}

      {tab === "adapters" && (
        <div className="panel">
          <h2>Adapter management</h2>
          <ul className="admin-list">
            {adapters.map((a) => (
              <li key={a.id}>
                <div>
                  <strong>{a.name}</strong>{" "}
                  <span className="mono muted">{a.id}</span>
                  {a.active ? <span className="pill">active</span> : null}
                  {a.description ? <p className="muted">{a.description}</p> : null}
                </div>
                <div className="admin-actions">
                  <button type="button" disabled={busy || a.active} onClick={() => activate(a.id)}>
                    Activate
                  </button>
                  <button type="button" disabled={busy} onClick={() => removeAdapter(a.id)}>
                    Remove
                  </button>
                </div>
              </li>
            ))}
          </ul>
          <button type="button" disabled={busy} onClick={() => activate("")}>
            Clear active adapter
          </button>

          <form className="admin-form" onSubmit={onAddAdapter}>
            <h3>Register adapter</h3>
            <div className="field">
              <label htmlFor="ad-id">ID</label>
              <input id="ad-id" value={newId} onChange={(e) => setNewId(e.target.value)} required />
            </div>
            <div className="field">
              <label htmlFor="ad-name">Name</label>
              <input id="ad-name" value={newName} onChange={(e) => setNewName(e.target.value)} />
            </div>
            <div className="field">
              <label htmlFor="ad-desc">Description</label>
              <input id="ad-desc" value={newDesc} onChange={(e) => setNewDesc(e.target.value)} />
            </div>
            <button type="submit" className="btn" disabled={busy}>
              Upsert
            </button>
          </form>
        </div>
      )}

      {tab === "prompt" && config && (
        <form
          className="panel"
          onSubmit={(e) => {
            e.preventDefault();
            void saveConfig({ system_prompt: config.system_prompt });
          }}
        >
          <h2>System prompt</h2>
          <div className="field">
            <label htmlFor="sys-prompt">Live system character</label>
            <textarea
              id="sys-prompt"
              rows={8}
              value={config.system_prompt}
              onChange={(e) => setConfig({ ...config, system_prompt: e.target.value })}
            />
          </div>
          <button type="submit" className="btn" disabled={busy}>
            Apply hot-swap
          </button>
        </form>
      )}

      {tab === "limits" && config && (
        <form
          className="panel"
          onSubmit={(e) => {
            e.preventDefault();
            void saveConfig({
              max_tokens: config.max_tokens,
              temperature: config.temperature,
              top_p: config.top_p,
            });
          }}
        >
          <h2>Context limits</h2>
          <div className="field">
            <label htmlFor="max-tok">Max tokens</label>
            <input
              id="max-tok"
              type="number"
              min={16}
              max={8192}
              value={config.max_tokens}
              onChange={(e) => setConfig({ ...config, max_tokens: Number(e.target.value) })}
            />
          </div>
          <div className="field">
            <label htmlFor="temp">Temperature</label>
            <input
              id="temp"
              type="number"
              step="0.05"
              min={0}
              max={2}
              value={config.temperature}
              onChange={(e) => setConfig({ ...config, temperature: Number(e.target.value) })}
            />
          </div>
          <div className="field">
            <label htmlFor="top-p">Top-P</label>
            <input
              id="top-p"
              type="number"
              step="0.05"
              min={0.05}
              max={1}
              value={config.top_p}
              onChange={(e) => setConfig({ ...config, top_p: Number(e.target.value) })}
            />
          </div>
          <button type="submit" className="btn" disabled={busy}>
            Apply hot-swap
          </button>
        </form>
      )}

      {tab === "logs" && (
        <div className="panel">
          <h2>Log monitor</h2>
          <button type="button" onClick={() => void refresh()} disabled={busy}>
            Refresh
          </button>
          <table className="rich-table" style={{ marginTop: "1rem" }}>
            <thead>
              <tr>
                <th>Time</th>
                <th>Tool</th>
                <th>Latency</th>
                <th>OK</th>
                <th>Query</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((row) => (
                <tr key={row.id}>
                  <td className="mono">{new Date(row.created_at).toLocaleTimeString()}</td>
                  <td>{row.tool}</td>
                  <td className="mono">{row.latency_ms}ms</td>
                  <td>{row.ok ? "yes" : "no"}</td>
                  <td>{row.query}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {logs.length === 0 ? <p className="muted">No MCP queries yet.</p> : null}
        </div>
      )}

      {tab === "finetune" && (
        <div className="panel">
          <h2>Human-feedback fine-tune</h2>
          <p className="muted">
            Export labeled reviews as chat JSONL, train a LoRA under{" "}
            <code>training/train_lora.py</code>, then activate the adapter here.
          </p>
          <p>
            Labeled examples available:{" "}
            <strong className="mono">{finetuneCount == null ? "…" : finetuneCount}</strong>
          </p>
          <div className="admin-actions">
            <button
              type="button"
              className="btn"
              disabled={busy}
              onClick={async () => {
                if (!accessToken) return;
                setBusy(true);
                setError(null);
                try {
                  const text = await adminApi.downloadFinetuneJsonl(accessToken);
                  const blob = new Blob([text], { type: "application/x-ndjson" });
                  const url = URL.createObjectURL(blob);
                  const a = document.createElement("a");
                  a.href = url;
                  a.download = "feedback-finetune.jsonl";
                  a.click();
                  URL.revokeObjectURL(url);
                  setNotice("Downloaded feedback-finetune.jsonl — see training/README.md");
                } catch (err) {
                  setError(err instanceof Error ? err.message : "Export failed");
                } finally {
                  setBusy(false);
                }
              }}
            >
              Download JSONL
            </button>
            <button type="button" disabled={busy} onClick={() => void refresh()}>
              Refresh count
            </button>
          </div>
          <ol className="muted" style={{ marginTop: "1rem" }}>
            <li>Label reviews in Simulator / Dashboard feedback</li>
            <li>Download JSONL (or use <code>training/export_feedback.py</code>)</li>
            <li>
              Run <code>python train_lora.py --data … --adapter-id feedback-lora</code>
            </li>
            <li>Admin → Adapters → Activate <code>feedback-lora</code></li>
          </ol>
        </div>
      )}
    </div>
  );
}
