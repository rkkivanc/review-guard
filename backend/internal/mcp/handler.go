package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/adapters"
	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/llm"
	"github.com/masterfabric/review-guard/mf-backend/internal/llqlog"
	"github.com/masterfabric/review-guard/mf-backend/internal/llmruntime"
	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

// Handler serves a JSON-RPC-style MCP endpoint over HTTP.
type Handler struct {
	Tokens  *auth.TokenManager
	AuthSvc *service.AuthService
	LLM     *llm.Client
	Runtime *llmruntime.Store
	Adapters *adapters.Registry
	Logs    *llqlog.Store
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ServeHTTP implements MCP JSON-RPC (tools/list, tools/call).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRPC(w, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32600, Message: "POST required"},
		})
		return
	}

	userID, err := h.authenticate(r)
	if err != nil {
		writeRPC(w, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32001, Message: "unauthorized"},
		})
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPC(w, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		})
		return
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	switch req.Method {
	case "initialize":
		writeRPC(w, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]string{"name": "reviewguard-mcp", "version": "1.0.0"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			},
		})
	case "tools/list":
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": toolDefs()}})
	case "tools/call":
		var p toolsCallParams
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			writeRPC(w, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32602, Message: "invalid tools/call params"},
			})
			return
		}
		result, err := h.callTool(r.Context(), userID, p.Name, p.Arguments)
		if err != nil {
			writeRPC(w, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32000, Message: err.Error()},
			})
			return
		}
		writeRPC(w, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{{"type": "text", "text": result.Content}},
				"structuredContent": result,
				"isError":           false,
			},
		})
	default:
		writeRPC(w, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: "method not found"},
		})
	}
}

func (h *Handler) authenticate(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" || !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return "", domain.ErrUnauthorized
	}
	raw := strings.TrimSpace(header[7:])
	claims, err := h.Tokens.ParseAccessToken(raw)
	if err != nil || claims.UserID == "" {
		return "", domain.ErrUnauthorized
	}
	if err := h.AuthSvc.ValidateAccessClaims(r.Context(), claims.UserID, claims.TokenVersion); err != nil {
		return "", domain.ErrUnauthorized
	}
	return claims.UserID, nil
}

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "deepkwiki.search",
			"description": "DeepKwiki search: enrich query with static specs and return a rich Markdown result",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"query": map[string]any{"type": "string"}},
				"required":   []string{"query"},
			},
		},
		{
			"name":        "llm.classify",
			"description": "Classify a game review via the local MLC LLM + active PEFT adapter",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"game_name":   map[string]any{"type": "string"},
					"stars":       map[string]any{"type": "integer"},
					"review_text": map[string]any{"type": "string"},
				},
				"required": []string{"game_name", "stars", "review_text"},
			},
		},
		{
			"name":        "llm.complete",
			"description": "Free-form completion with runtime system prompt / sampling settings",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"prompt": map[string]any{"type": "string"}},
				"required":   []string{"prompt"},
			},
		},
	}
}

func (h *Handler) callTool(ctx context.Context, userID, name string, args map[string]any) (domain.RichResult, error) {
	rt := h.Runtime.Get()
	started := time.Now()
	var (
		content string
		format  = "markdown"
		query   string
		err     error
	)

	switch name {
	case "deepkwiki.search":
		query = strArg(args, "query")
		if query == "" {
			return domain.RichResult{}, fmt.Errorf("%w: query required", domain.ErrValidation)
		}
		prompt := buildDeepKwikiPrompt(query, rt.SystemPrompt)
		content, err = h.LLM.Complete(ctx, llm.CompleteOpts{
			SystemPrompt: rt.SystemPrompt,
			UserPrompt:   prompt,
			Temperature:  rt.Temperature,
			TopP:         rt.TopP,
			MaxTokens:    rt.MaxTokens,
			AdapterID:    rt.ActiveAdapter,
		})
	case "llm.complete":
		query = strArg(args, "prompt")
		if query == "" {
			return domain.RichResult{}, fmt.Errorf("%w: prompt required", domain.ErrValidation)
		}
		content, err = h.LLM.Complete(ctx, llm.CompleteOpts{
			SystemPrompt: rt.SystemPrompt,
			UserPrompt:   query,
			Temperature:  rt.Temperature,
			TopP:         rt.TopP,
			MaxTokens:    rt.MaxTokens,
			AdapterID:    rt.ActiveAdapter,
		})
	case "llm.classify":
		game := strArg(args, "game_name")
		review := strArg(args, "review_text")
		stars := intArg(args, "stars")
		query = fmt.Sprintf("%s / %d★", game, stars)
		if game == "" || review == "" || stars < 1 || stars > 10 {
			return domain.RichResult{}, fmt.Errorf("%w: game_name, stars (1-10), review_text required", domain.ErrValidation)
		}
		res, cerr := h.LLM.ClassifyThreeTimes(ctx, llm.ClassifyInput{
			GameName:     game,
			Stars:        stars,
			ReviewText:   review,
			AdapterID:    rt.ActiveAdapter,
			Temperature:  rt.Temperature,
			SystemPrompt: rt.SystemPrompt,
		})
		err = cerr
		if err == nil {
			raw, _ := json.MarshalIndent(res.Runs, "", "  ")
			content = string(raw)
			format = "json"
		}
	default:
		return domain.RichResult{}, fmt.Errorf("%w: unknown tool %s", domain.ErrValidation, name)
	}

	latency := int(time.Since(started).Milliseconds())
	if err != nil {
		h.Logs.Add(userID, name, query, rt.ActiveAdapter, h.LLM.ModelID(), latency, false, err.Error())
		return domain.RichResult{}, err
	}
	h.Logs.Add(userID, name, query, rt.ActiveAdapter, h.LLM.ModelID(), latency, true, "")

	return domain.RichResult{
		Content:   content,
		Format:    format,
		LatencyMS: latency,
		Metadata: map[string]any{
			"tool":       name,
			"adapter_id": rt.ActiveAdapter,
			"model_id":   h.LLM.ModelID(),
			"temperature": rt.Temperature,
			"top_p":      rt.TopP,
			"max_tokens": rt.MaxTokens,
		},
	}, nil
}

func buildDeepKwikiPrompt(query, systemPrompt string) string {
	var b strings.Builder
	b.WriteString("DeepKwiki request. Use the static specifications below, then answer the user query.\n\n")
	b.WriteString("## Static specifications\n")
	b.WriteString("- Product: ReviewGuard local LLM stack (MLC + PEFT + MCP)\n")
	b.WriteString("- Roles: user, admin\n")
	b.WriteString("- Dimensions: consistency, authenticity, experience, usefulness\n")
	b.WriteString("- Transport: WebMCP → Go backend → MLC-LLM\n")
	if systemPrompt != "" {
		b.WriteString("\n## System character\n")
		b.WriteString(systemPrompt)
		b.WriteByte('\n')
	}
	b.WriteString("\n## User query\n")
	b.WriteString(query)
	b.WriteString("\n\nRespond in Markdown. Include at least one table when listing structured facts.\n")
	return b.String()
}

func strArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func intArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		var n int
		_, _ = fmt.Sscanf(fmt.Sprint(t), "%d", &n)
		return n
	}
}

func writeRPC(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if resp.Error != nil && resp.Result == nil {
		if resp.Error.Code == -32001 {
			w.WriteHeader(http.StatusUnauthorized)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(resp)
}
