"use client";

export function SimulatorView() {
  return (
    <div className="stub">
      <h2>Review Simulator</h2>
      <p className="muted">
        Next step: load Gemma in the browser with <code>@mlc-ai/web-llm</code>, classify a review
        three times, then POST the runs to the Go API for trust scoring.
      </p>
      <p className="muted">Model target: gemma-2-2b-it-q4f16_1-MLC (WebGPU).</p>
    </div>
  );
}
