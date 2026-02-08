package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dublyo/mcp-gateway/internal/gateway"
	"github.com/dublyo/mcp-gateway/internal/mcp"
)

// Session tracks an active SSE or Streamable HTTP session
type Session struct {
	ID       string
	ConnID   string
	Messages chan []byte // SSE events sent to client
	done     chan struct{}
}

// Server is the HTTP server that handles MCP requests
type Server struct {
	gw       *gateway.Gateway
	sessions sync.Map // sessionID -> *Session
}

func New(gw *gateway.Gateway) *Server {
	return &Server{gw: gw}
}

func (s *Server) Start() error {
	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleRequest)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // SSE needs no write timeout
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[server] listening on :%s", port)
	return server.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Extract domain from Host header
	host := r.Host
	if idx := strings.Index(host, ":"); idx > 0 {
		host = host[:idx]
	}

	conn := s.gw.GetConnection(host)
	if conn == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	path := r.URL.Path

	// Route to transport
	switch {
	case path == "/sse" && r.Method == "GET":
		s.handleSSE(w, r, conn)
	case path == "/message" && r.Method == "POST":
		s.handleSSEMessage(w, r, conn)
	case path == "/mcp" && r.Method == "POST":
		s.handleStreamableHTTP(w, r, conn)
	case path == "/mcp" && r.Method == "GET":
		s.handleStreamableSSE(w, r, conn)
	case path == "/mcp" && r.Method == "DELETE":
		s.handleStreamableDelete(w, r, conn)
	default:
		http.Error(w, "Not Found", http.StatusNotFound)
	}
}

// authenticateRequest validates the Bearer token
func (s *Server) authenticateRequest(w http.ResponseWriter, r *http.Request, conn *gateway.Connection) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		s.gw.RecordAuthFailure(conn.Config.ID)
		return false
	}

	apiKey := strings.TrimPrefix(auth, "Bearer ")
	if !s.gw.VerifyAPIKey(conn, apiKey) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		s.gw.RecordAuthFailure(conn.Config.ID)
		return false
	}

	// Rate limit check
	if !s.gw.CheckRateLimit(conn) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return false
	}

	return true
}

// ========== SSE Transport (Claude Desktop compatible) ==========

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request, conn *gateway.Connection) {
	if !s.authenticateRequest(w, r, conn) {
		return
	}

	// Check concurrency
	if !s.gw.CheckConcurrency(conn) {
		http.Error(w, "Too many concurrent sessions", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Create session
	sessionID := generateSessionID()
	session := &Session{
		ID:       sessionID,
		ConnID:   conn.Config.ID,
		Messages: make(chan []byte, 64),
		done:     make(chan struct{}),
	}
	s.sessions.Store(sessionID, session)
	s.gw.IncrementSessions(conn)

	defer func() {
		close(session.done)
		s.sessions.Delete(sessionID)
		s.gw.DecrementSessions(conn)
	}()

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send endpoint event
	messageURL := fmt.Sprintf("/message?sessionId=%s", sessionID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", messageURL)
	flusher.Flush()

	// Keep connection alive, send messages
	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-session.done:
			return
		case msg := <-session.Messages:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(msg))
			flusher.Flush()
		case <-keepAlive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleSSEMessage(w http.ResponseWriter, r *http.Request, conn *gateway.Connection) {
	if !s.authenticateRequest(w, r, conn) {
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "Missing sessionId", http.StatusBadRequest)
		return
	}

	sessionVal, ok := s.sessions.Load(sessionID)
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	session := sessionVal.(*Session)

	// Read request body
	body := make([]byte, 0, 1024)
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if err != nil {
			break
		}
		if len(body) > 1024*1024 { // 1MB limit
			http.Error(w, "Request too large", http.StatusRequestEntityTooLarge)
			return
		}
	}

	start := time.Now()

	// Process the message
	response := conn.Handler.HandleMessage(body)
	latency := float64(time.Since(start).Milliseconds())

	isError := response != nil && response.Error != nil
	s.gw.RecordRequest(conn.Config.ID, latency, isError)

	if response != nil {
		respBytes, _ := json.Marshal(response)
		select {
		case session.Messages <- respBytes:
		default:
			log.Printf("[server] session %s message buffer full, dropping", sessionID)
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

// ========== Streamable HTTP Transport ==========

func (s *Server) handleStreamableHTTP(w http.ResponseWriter, r *http.Request, conn *gateway.Connection) {
	if !s.authenticateRequest(w, r, conn) {
		return
	}

	// Read body
	body := make([]byte, 0, 1024)
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if err != nil {
			break
		}
		if len(body) > 1024*1024 {
			http.Error(w, "Request too large", http.StatusRequestEntityTooLarge)
			return
		}
	}

	// Check if this is a notification (no id field)
	var rawMsg map[string]interface{}
	if err := json.Unmarshal(body, &rawMsg); err == nil {
		if _, hasID := rawMsg["id"]; !hasID {
			// Notification — no response needed
			conn.Handler.HandleMessage(body)
			w.WriteHeader(http.StatusAccepted)
			return
		}
	}

	start := time.Now()
	response := conn.Handler.HandleMessage(body)
	latency := float64(time.Since(start).Milliseconds())

	isError := response != nil && response.Error != nil
	s.gw.RecordRequest(conn.Config.ID, latency, isError)

	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Get or create session ID
	sessionID := r.Header.Get("mcp-session-id")
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("mcp-session-id", sessionID)
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleStreamableSSE(w http.ResponseWriter, r *http.Request, conn *gateway.Connection) {
	if !s.authenticateRequest(w, r, conn) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Keep alive until client disconnects
	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleStreamableDelete(w http.ResponseWriter, r *http.Request, conn *gateway.Connection) {
	// Session termination
	sessionID := r.Header.Get("mcp-session-id")
	if sessionID != "" {
		if sessionVal, ok := s.sessions.Load(sessionID); ok {
			session := sessionVal.(*Session)
			close(session.done)
			s.sessions.Delete(sessionID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// generateSessionID creates a short unique session ID
func generateSessionID() string {
	return fmt.Sprintf("s_%d_%d", time.Now().UnixNano(), time.Now().UnixMicro()%10000)
}

// JSONRPCResponse for direct responses
type jsonrpcBatchItem struct {
	ID     interface{}      `json:"id"`
	Result *mcp.JSONRPCResponse `json:"result,omitempty"`
}
