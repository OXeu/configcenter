package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/auth"
)

const (
	jsonRPCVersion     = "2.0"
	mcpProtocol        = "2025-06-18"
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResult struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MountMCP serves all API operations over a single stateless MCP endpoint.
func (s *Server) MountMCP(r *gin.Engine) {
	s.mcpAPI = gin.New()
	s.mcpAPI.UseRawPath = true
	s.mcpAPI.UnescapePathValues = true
	s.mcpAPI.Use(s.authenticator.BrowserMiddleware())
	s.MountAPI(s.mcpAPI)
	metadataURL := ""
	if s.oauth != nil {
		metadataURL = s.cfg.PublicURL + "/.well-known/oauth-protected-resource/mcp"
	}
	middleware := s.authenticator.MCPMiddleware(s.cfg.PublicURL+"/mcp", metadataURL)
	r.POST("/mcp", s.mcpOrigin, middleware, s.mcpHandle)
	r.Match([]string{http.MethodGet, http.MethodDelete}, "/mcp", s.mcpOrigin, middleware, func(c *gin.Context) {
		c.Header("Allow", http.MethodPost)
		c.Status(http.StatusMethodNotAllowed)
	})
}

func (s *Server) mcpOrigin(c *gin.Context) {
	if origin := c.GetHeader("Origin"); origin != "" {
		public, err := url.Parse(s.cfg.PublicURL)
		if err != nil || public.Host == "" || !strings.EqualFold(origin, public.Scheme+"://"+public.Host) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid origin"})
			return
		}
	}
	c.Next()
}

func (s *Server) mcpHandle(c *gin.Context) {
	if version := c.GetHeader("MCP-Protocol-Version"); version != "" && version != mcpProtocol {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported MCP protocol version"})
		return
	}
	var req rpcRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	if err := decoder.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, rpcResult{JSONRPC: jsonRPCVersion, Error: &rpcError{Code: codeParseError, Message: "parse error"}})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		c.JSON(http.StatusBadRequest, rpcResult{JSONRPC: jsonRPCVersion, Error: &rpcError{Code: codeParseError, Message: "expected one request"}})
		return
	}
	if req.JSONRPC != jsonRPCVersion || req.Method == "" || !validRPCID(req.ID) {
		c.JSON(http.StatusOK, rpcResult{JSONRPC: jsonRPCVersion, Error: &rpcError{Code: codeInvalidRequest, Message: "invalid request"}})
		return
	}
	if len(req.ID) == 0 {
		c.Status(http.StatusAccepted)
		return
	}
	result, rpcErr := s.dispatch(c, req)
	c.JSON(http.StatusOK, rpcResult{JSONRPC: jsonRPCVersion, ID: req.ID, Result: result, Error: rpcErr})
}

func validRPCID(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var id any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&id); err != nil {
		return false
	}
	switch id.(type) {
	case string, json.Number:
		return true
	default:
		return false
	}
}

func (s *Server) dispatch(c *gin.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return gin.H{
			"protocolVersion": mcpProtocol,
			"capabilities":    gin.H{"tools": gin.H{"listChanged": false}},
			"serverInfo":      gin.H{"name": "configcenter", "version": "1.0.0"},
		}, nil
	case "ping":
		return gin.H{}, nil
	case "tools/list":
		return gin.H{"tools": s.mcpTools(c)}, nil
	case "tools/call":
		var call struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		decoder := json.NewDecoder(bytes.NewReader(req.Params))
		decoder.UseNumber()
		if err := decoder.Decode(&call); err != nil || call.Name == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params"}
		}
		return s.callTool(c, call.Name, call.Arguments)
	case "resources/list":
		return gin.H{"resources": []any{}}, nil
	case "prompts/list":
		return gin.H{"prompts": []any{}}, nil
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found"}
	}
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func schema(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func (op apiOperation) inputSchema() map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, arg := range op.Arguments {
		props[arg.Name] = arg.Schema
		if arg.Required {
			required = append(required, arg.Name)
		}
	}
	return schema(props, required...)
}

func (s *Server) mcpTools(c *gin.Context) []mcpTool {
	id, _ := auth.FromContext(c.Request.Context())
	out := []mcpTool{}
	for _, op := range s.apiOperations() {
		if op.allowed(id) {
			out = append(out, mcpTool{Name: op.Name, Description: op.Description, InputSchema: op.inputSchema()})
		}
	}
	return out
}

func (s *Server) callTool(c *gin.Context, name string, args map[string]any) (any, *rpcError) {
	for _, op := range s.apiOperations() {
		if op.Name != name {
			continue
		}
		if args == nil {
			args = map[string]any{}
		}
		if err := validateToolValue(args, op.inputSchema(), "arguments"); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
		}
		return s.executeTool(c, op, args)
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool"}
}

func (s *Server) executeTool(c *gin.Context, op apiOperation, args map[string]any) (any, *rpcError) {
	segments := strings.Split("/api/v1"+op.Path, "/")
	parameters := map[string]string{}
	query := url.Values{}
	body := map[string]any{}
	for _, arg := range op.Arguments {
		value, exists := args[arg.Name]
		if !exists {
			continue
		}
		switch arg.Location {
		case "path":
			parameters[":"+arg.Target] = url.PathEscape(argumentText(value))
		case "query":
			if values, ok := value.([]any); ok {
				for _, entry := range values {
					query.Add(arg.Target, argumentText(entry))
				}
			} else {
				query.Set(arg.Target, argumentText(value))
			}
		case "body":
			body[arg.Target] = value
		}
	}
	for index, segment := range segments {
		if value, ok := parameters[segment]; ok {
			segments[index] = value
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, &rpcError{Code: codeInternal, Message: "encode request"}
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), op.Method, strings.Join(segments, "/")+"?"+query.Encode(), bytes.NewReader(encoded))
	if err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid path arguments"}
	}
	request.Header = c.Request.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	response := &apiResponse{header: make(http.Header), status: http.StatusOK}
	s.mcpAPI.ServeHTTP(response, request)
	var result map[string]any
	switch {
	case len(response.body) > 0:
		if err := json.Unmarshal(response.body, &result); err != nil {
			return nil, &rpcError{Code: codeInternal, Message: "invalid API response"}
		}
	case response.status >= http.StatusBadRequest:
		result = map[string]any{"error": http.StatusText(response.status)}
	default:
		result = map[string]any{"success": true}
	}
	result["http_status"] = response.status
	text, err := json.Marshal(result)
	if err != nil {
		return nil, &rpcError{Code: codeInternal, Message: "encode result"}
	}
	return gin.H{"content": []gin.H{{"type": "text", "text": string(text)}}, "structuredContent": result, "isError": response.status >= http.StatusBadRequest}, nil
}

func argumentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return string(typed)
	default:
		return ""
	}
}

type apiResponse struct {
	header http.Header
	status int
	body   []byte
}

// Header returns the API response headers.
func (r *apiResponse) Header() http.Header { return r.header }

// WriteHeader records the API response status.
func (r *apiResponse) WriteHeader(status int) { r.status = status }

// Write buffers the API response body.
func (r *apiResponse) Write(body []byte) (int, error) {
	r.body = append(r.body, body...)
	return len(body), nil
}
