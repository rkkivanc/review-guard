"use client";

import { useMemo } from "react";
import type { RichResult } from "@/lib/webmcp";

type ChartSpec = {
  type: string;
  title?: string;
  labels: string[];
  values: number[];
};

function parseChartBlocks(markdown: string): { body: string; charts: ChartSpec[] } {
  const charts: ChartSpec[] = [];
  const body = markdown.replace(/```chart\s*([\s\S]*?)```/g, (_, raw: string) => {
    try {
      const parsed = JSON.parse(raw.trim()) as ChartSpec;
      if (parsed?.labels && parsed?.values) {
        charts.push(parsed);
        return `\n\n*[chart: ${parsed.title || parsed.type}]*\n\n`;
      }
    } catch {
      /* ignore bad chart blocks */
    }
    return "";
  });
  return { body, charts };
}

function renderMarkdownLite(md: string): string {
  let html = md
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

  html = html.replace(/^### (.+)$/gm, "<h3>$1</h3>");
  html = html.replace(/^## (.+)$/gm, "<h2>$1</h2>");
  html = html.replace(/^# (.+)$/gm, "<h1>$1</h1>");
  html = html.replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>");
  html = html.replace(/`([^`]+)`/g, "<code>$1</code>");
  html = html.replace(/^\d+\.\s+(.+)$/gm, "<li>$1</li>");
  html = html.replace(/^[-*]\s+(.+)$/gm, "<li>$1</li>");

  // Simple GFM tables
  html = html.replace(/(?:^|\n)(\|.+\|)\n(\|[-:| ]+\|)\n((?:\|.+\|\n?)*)/g, (_m, header, _sep, rows) => {
    const th = String(header)
      .split("|")
      .map((c) => c.trim())
      .filter(Boolean)
      .map((c) => `<th>${c}</th>`)
      .join("");
    const trs = String(rows)
      .trim()
      .split("\n")
      .filter(Boolean)
      .map((row) => {
        const tds = row
          .split("|")
          .map((c) => c.trim())
          .filter(Boolean)
          .map((c) => `<td>${c}</td>`)
          .join("");
        return `<tr>${tds}</tr>`;
      })
      .join("");
    return `<table class="rich-table"><thead><tr>${th}</tr></thead><tbody>${trs}</tbody></table>`;
  });

  html = html.replace(/\n{2,}/g, "</p><p>");
  return `<p>${html}</p>`;
}

function BarChart({ chart }: { chart: ChartSpec }) {
  const max = Math.max(...chart.values, 1);
  return (
    <div className="rich-chart panel">
      {chart.title ? <h3>{chart.title}</h3> : null}
      <div className="rich-bars" role="img" aria-label={chart.title || "chart"}>
        {chart.labels.map((label, i) => {
          const v = chart.values[i] ?? 0;
          const pct = Math.round((v / max) * 100);
          return (
            <div key={label} className="rich-bar-row">
              <span className="rich-bar-label">{label}</span>
              <div className="rich-bar-track">
                <div className="rich-bar-fill" style={{ width: `${pct}%` }} />
              </div>
              <span className="rich-bar-value mono">{v}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export function RichResultView({ result }: { result: RichResult }) {
  const { body, charts } = useMemo(
    () => parseChartBlocks(result.content || ""),
    [result.content],
  );
  const html = useMemo(() => {
    if (result.format === "json") {
      return `<pre class="mono">${body.replace(/&/g, "&amp;").replace(/</g, "&lt;")}</pre>`;
    }
    return renderMarkdownLite(body);
  }, [body, result.format]);

  const meta = result.metadata || {};

  return (
    <div className="rich-result">
      <div className="rich-meta muted">
        <span>{result.latency_ms} ms</span>
        {meta.model_id ? <span> · model {String(meta.model_id)}</span> : null}
        {meta.adapter_id ? <span> · adapter {String(meta.adapter_id)}</span> : null}
        {meta.tool ? <span> · {String(meta.tool)}</span> : null}
      </div>
      <div className="panel rich-body" dangerouslySetInnerHTML={{ __html: html }} />
      {charts.map((c, i) => (
        <BarChart key={`${c.title || "chart"}-${i}`} chart={c} />
      ))}
    </div>
  );
}
