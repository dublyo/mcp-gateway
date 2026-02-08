package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/dublyo/mcp-gateway/internal/profiles"
)

// Handler processes MCP JSON-RPC messages for a specific profile
type Handler struct {
	profile profiles.Profile
	envVars map[string]string
}

func NewHandler(profile profiles.Profile, envVars map[string]string) *Handler {
	return &Handler{profile: profile, envVars: envVars}
}

// HandleMessage processes a JSON-RPC request and returns a response
func (h *Handler) HandleMessage(raw []byte) *JSONRPCResponse {
	var req JSONRPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &JSONRPCError{Code: ParseError, Message: "Parse error"},
		}
	}

	if req.JSONRPC != "2.0" {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: InvalidRequest, Message: "Invalid JSON-RPC version"},
		}
	}

	switch req.Method {
	case "initialize":
		return h.handleInitialize(req)
	case "initialized":
		// Notification, no response needed
		return nil
	case "ping":
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{}}
	case "tools/list":
		return h.handleToolsList(req)
	case "tools/call":
		return h.handleToolsCall(req)
	case "notifications/cancelled":
		return nil
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: MethodNotFound, Message: fmt.Sprintf("Method not found: %s", req.Method)},
		}
	}
}

func (h *Handler) handleInitialize(req JSONRPCRequest) *JSONRPCResponse {
	// Parse params to check protocol version
	if req.Params != nil {
		paramsBytes, _ := json.Marshal(req.Params)
		var params InitializeParams
		if err := json.Unmarshal(paramsBytes, &params); err == nil {
			if params.ProtocolVersion != "" && params.ProtocolVersion != ProtocolVersion {
				return &JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &JSONRPCError{
						Code:    InvalidParams,
						Message: fmt.Sprintf("Unsupported protocol version: %s. Supported: %s", params.ProtocolVersion, ProtocolVersion),
					},
				}
			}
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: Capabilities{
				Tools: &ToolsCapability{},
			},
			ServerInfo: ServerInfo{
				Name:    "dublyo-mcp-gateway",
				Version: "1.0.0",
			},
		},
	}
}

func (h *Handler) handleToolsList(req JSONRPCRequest) *JSONRPCResponse {
	tools := h.profile.Tools()
	defs := make([]ToolDef, len(tools))
	for i, t := range tools {
		defs[i] = ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  ToolsListResult{Tools: defs},
	}
}

func (h *Handler) handleToolsCall(req JSONRPCRequest) *JSONRPCResponse {
	paramsBytes, _ := json.Marshal(req.Params)
	var params ToolCallParams
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: InvalidParams, Message: "Invalid tool call params"},
		}
	}

	result, err := h.profile.CallTool(params.Name, params.Arguments, h.envVars)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolCallResult{
				Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error: %s", err.Error())}},
				IsError: true,
			},
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: ToolCallResult{
			Content: []ContentBlock{{Type: "text", Text: result}},
		},
	}
}
