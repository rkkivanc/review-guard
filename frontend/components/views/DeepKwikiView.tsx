"use client";

import { useState, type FormEvent } from "react";
import { RichResultView } from "@/components/RichResultView";
import { useAuth } from "@/lib/auth";
import { webmcp, type RichResult } from "@/lib/webmcp";

export function DeepKwikiView() {
  const { accessToken } = useAuth();
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<RichResult | null>(null);

  async function onSearch(e: FormEvent) {
    e.preventDefault();
    setError(null);
    if (!accessToken) {
      setError("Session expired. Sign in again.");
      return;
    }
    const q = query.trim();
    if (!q) {
      setError("Enter a DeepKwiki query.");
      return;
    }
    setBusy(true);
    try {
      const rich = await webmcp.deepKwikiSearch(accessToken, q);
      setResult(rich);
    } catch (err) {
      setError(err instanceof Error ? err.message : "DeepKwiki request failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <h1>DeepKwiki</h1>
      <p className="muted">
        WebMCP packages your query with static ReviewGuard specs, then the Go backend proxies to
        the local MLC-LLM engine (active PEFT adapter applied).
      </p>

      <form className="panel" onSubmit={onSearch}>
        <div className="field">
          <label htmlFor="dk-query">Query</label>
          <textarea
            id="dk-query"
            rows={4}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Explain authenticity vs experience dimensions…"
            required
          />
        </div>
        <button type="submit" className="btn" disabled={busy}>
          {busy ? "Searching…" : "Search via WebMCP"}
        </button>
      </form>

      {error ? <div className="error-box">{error}</div> : null}
      {result ? (
        <div style={{ marginTop: "1rem" }}>
          <h2>Rich result</h2>
          <RichResultView result={result} />
        </div>
      ) : null}
    </div>
  );
}
