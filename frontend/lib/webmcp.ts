import { getApiUrl } from "@/lib/api";

export type RichResult = {
  content: string;
  format: string;
  metadata: Record<string, unknown>;
  latency_ms: number;
};

type RpcError = { code: number; message: string };

type RpcResponse = {
  jsonrpc: string;
  id: number | string | null;
  result?: {
    content?: Array<{ type: string; text?: string }>;
    structuredContent?: RichResult;
    tools?: unknown[];
  };
  error?: RpcError;
};

let rpcId = 1;

async function mcpRpc(
  accessToken: string,
  method: string,
  params?: Record<string, unknown>,
): Promise<RpcResponse["result"]> {
  const id = rpcId++;
  const res = await fetch(`${getApiUrl()}/mcp`, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      Authorization: `Bearer ${accessToken}`,
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id,
      method,
      params: params ?? {},
    }),
  });
  const body = (await res.json()) as RpcResponse;
  if (body.error) {
    throw new Error(body.error.message || `MCP error ${body.error.code}`);
  }
  return body.result;
}

/** Compile static DeepKwiki specs + user query into an MCP tools/call. */
export function packageDeepKwikiQuery(query: string) {
  return {
    name: "deepkwiki.search",
    arguments: {
      query: query.trim(),
      static_specs: {
        product: "ReviewGuard",
        roles: ["user", "admin"],
        dimensions: ["consistency", "authenticity", "experience", "usefulness"],
        transport: "WebMCP→Go→MLC-LLM",
      },
    },
  };
}

export const webmcp = {
  async listTools(accessToken: string) {
    const result = await mcpRpc(accessToken, "tools/list");
    return result?.tools ?? [];
  },

  async callTool(
    accessToken: string,
    name: string,
    args: Record<string, unknown>,
  ): Promise<RichResult> {
    const result = await mcpRpc(accessToken, "tools/call", {
      name,
      arguments: args,
    });
    if (result?.structuredContent) {
      return result.structuredContent;
    }
    const text = result?.content?.map((c) => c.text || "").join("\n") || "";
    return {
      content: text,
      format: "markdown",
      metadata: {},
      latency_ms: 0,
    };
  },

  async deepKwikiSearch(accessToken: string, query: string): Promise<RichResult> {
    const packed = packageDeepKwikiQuery(query);
    return webmcp.callTool(accessToken, packed.name, packed.arguments);
  },
};
