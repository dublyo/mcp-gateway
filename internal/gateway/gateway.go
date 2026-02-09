package gateway

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"github.com/dublyo/mcp-gateway/internal/mcp"
	"github.com/dublyo/mcp-gateway/internal/profiles"
)

// ConnectionConfig is a single MCP connection received from the API
type ConnectionConfig struct {
	ID             string            `json:"id"`
	Slug           string            `json:"slug"`
	Domain         string            `json:"domain"`
	Profile        string            `json:"profile"`
	APIKeyHash     string            `json:"apiKeyHash"`
	PrevKeyHash    string            `json:"prevKeyHash,omitempty"`
	PrevKeyExpiry  string            `json:"prevKeyExpiry,omitempty"`
	Enabled        bool              `json:"enabled"`
	EnvVars        map[string]string `json:"envVars"`
	RateLimit      int               `json:"rateLimit"`
	MaxConcurrency int               `json:"maxConcurrency"`
	CreatedAt      string            `json:"createdAt"`
}

// GatewayConfig is received from the Dublyo API sync endpoint
type GatewayConfig struct {
	ServerID    string             `json:"serverId"`
	GatewayID   string             `json:"gatewayId"`
	Pepper      string             `json:"pepper"`
	Connections []ConnectionConfig `json:"connections"`
	Version     int64              `json:"version"`
}

// Connection is a live connection with its MCP handler
type Connection struct {
	Config  ConnectionConfig
	Handler *mcp.Handler

	// Rate limiting
	mu          sync.Mutex
	requests    []time.Time
	sessions    int32
}

// Gateway manages all connections and their state
type Gateway struct {
	mu          sync.RWMutex
	connections map[string]*Connection // keyed by domain
	pepper      string
	version     int64
	gatewayID   string
	serverID    string

	// Metrics
	metricsMu sync.Mutex
	metrics   map[string]*Metrics
}

type Metrics struct {
	RequestCount  int64
	ErrorCount    int64
	AuthFailures  int64
	Latencies     []float64 // rolling window for P95
	ActiveSessions int
	LastRequestAt  time.Time
}

func New() *Gateway {
	return &Gateway{
		connections: make(map[string]*Connection),
		metrics:     make(map[string]*Metrics),
	}
}

// ApplyConfig applies a new config from the API
func (g *Gateway) ApplyConfig(cfg GatewayConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.pepper = cfg.Pepper
	g.version = cfg.Version
	g.gatewayID = cfg.GatewayID
	g.serverID = cfg.ServerID

	// Build new connection map
	newConns := make(map[string]*Connection)
	for _, cc := range cfg.Connections {
		if !cc.Enabled {
			continue
		}

		// Reuse existing connection if it exists and profile matches
		existing := g.connections[cc.Domain]
		if existing != nil && existing.Config.Profile == cc.Profile {
			existing.Config = cc
			existing.Handler.UpdateEnvVars(cc.EnvVars)
			newConns[cc.Domain] = existing
		} else {
			// Create new handler
			profile, ok := profiles.Get(cc.Profile)
			if !ok {
				log.Printf("Unknown profile %s for connection %s, skipping", cc.Profile, cc.Slug)
				continue
			}
			handler := mcp.NewHandler(profile, cc.EnvVars)
			newConns[cc.Domain] = &Connection{
				Config:  cc,
				Handler: handler,
			}
		}

		// Ensure metrics entry exists
		g.metricsMu.Lock()
		if _, ok := g.metrics[cc.ID]; !ok {
			g.metrics[cc.ID] = &Metrics{}
		}
		g.metricsMu.Unlock()
	}

	g.connections = newConns
	log.Printf("Config applied: version=%d, connections=%d", cfg.Version, len(newConns))
}

// GetConnection returns the connection for the given domain
func (g *Gateway) GetConnection(domain string) *Connection {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.connections[domain]
}

// Version returns the current config version
func (g *Gateway) Version() int64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.version
}

// VerifyAPIKey checks if the given API key is valid for the connection
func (g *Gateway) VerifyAPIKey(conn *Connection, apiKey string) bool {
	g.mu.RLock()
	pepper := g.pepper
	g.mu.RUnlock()

	// Hash: SHA-256(pepper + key)
	h := sha256.Sum256([]byte(pepper + apiKey))
	computed := hex.EncodeToString(h[:])

	// Check primary key
	if subtle.ConstantTimeCompare([]byte(computed), []byte(conn.Config.APIKeyHash)) == 1 {
		return true
	}

	// Check previous key during rotation grace period
	if conn.Config.PrevKeyHash != "" && conn.Config.PrevKeyExpiry != "" {
		expiry, err := time.Parse(time.RFC3339, conn.Config.PrevKeyExpiry)
		if err == nil && time.Now().Before(expiry) {
			if subtle.ConstantTimeCompare([]byte(computed), []byte(conn.Config.PrevKeyHash)) == 1 {
				return true
			}
		}
	}

	return false
}

// CheckRateLimit returns true if the request is within rate limits
func (g *Gateway) CheckRateLimit(conn *Connection) bool {
	limit := conn.Config.RateLimit
	if limit <= 0 {
		limit = 60
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Minute)

	// Remove expired entries
	valid := conn.requests[:0]
	for _, t := range conn.requests {
		if t.After(windowStart) {
			valid = append(valid, t)
		}
	}
	conn.requests = valid

	if len(conn.requests) >= limit {
		return false
	}

	conn.requests = append(conn.requests, now)
	return true
}

// CheckConcurrency returns true if under the concurrency limit
func (g *Gateway) CheckConcurrency(conn *Connection) bool {
	limit := conn.Config.MaxConcurrency
	if limit <= 0 {
		limit = 10
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return int(conn.sessions) < limit
}

// IncrementSessions increments the active session count
func (g *Gateway) IncrementSessions(conn *Connection) {
	conn.mu.Lock()
	conn.sessions++
	conn.mu.Unlock()
}

// DecrementSessions decrements the active session count
func (g *Gateway) DecrementSessions(conn *Connection) {
	conn.mu.Lock()
	if conn.sessions > 0 {
		conn.sessions--
	}
	conn.mu.Unlock()
}

// RecordRequest records a request metric
func (g *Gateway) RecordRequest(connID string, latencyMs float64, isError bool) {
	g.metricsMu.Lock()
	defer g.metricsMu.Unlock()

	m, ok := g.metrics[connID]
	if !ok {
		m = &Metrics{}
		g.metrics[connID] = m
	}

	m.RequestCount++
	m.LastRequestAt = time.Now()
	if isError {
		m.ErrorCount++
	}

	// Rolling latency window (keep last 100)
	m.Latencies = append(m.Latencies, latencyMs)
	if len(m.Latencies) > 100 {
		m.Latencies = m.Latencies[len(m.Latencies)-100:]
	}
}

// RecordAuthFailure records an auth failure
func (g *Gateway) RecordAuthFailure(connID string) {
	g.metricsMu.Lock()
	defer g.metricsMu.Unlock()

	m, ok := g.metrics[connID]
	if !ok {
		m = &Metrics{}
		g.metrics[connID] = m
	}
	m.AuthFailures++
}

// MetricsReport is what we send to the API
type MetricsReport struct {
	ConnectionID   string  `json:"connectionId"`
	RequestCount   int64   `json:"requestCount"`
	ErrorCount     int64   `json:"errorCount"`
	AuthFailures   int64   `json:"authFailures"`
	P95LatencyMs   float64 `json:"p95LatencyMs"`
	ActiveSessions int     `json:"activeSessions"`
	LastRequestAt  string  `json:"lastRequestAt,omitempty"`
}

// CollectAndResetMetrics returns current metrics and resets delta counters
func (g *Gateway) CollectAndResetMetrics() []MetricsReport {
	g.metricsMu.Lock()
	defer g.metricsMu.Unlock()

	g.mu.RLock()
	defer g.mu.RUnlock()

	var reports []MetricsReport
	for connID, m := range g.metrics {
		if m.RequestCount == 0 && m.ErrorCount == 0 && m.AuthFailures == 0 {
			continue
		}

		// Calculate P95
		p95 := float64(0)
		if len(m.Latencies) > 0 {
			sorted := make([]float64, len(m.Latencies))
			copy(sorted, m.Latencies)
			// Simple sort for P95
			for i := range sorted {
				for j := i + 1; j < len(sorted); j++ {
					if sorted[i] > sorted[j] {
						sorted[i], sorted[j] = sorted[j], sorted[i]
					}
				}
			}
			idx := int(float64(len(sorted)) * 0.95)
			if idx >= len(sorted) {
				idx = len(sorted) - 1
			}
			p95 = sorted[idx]
		}

		// Get active session count from connection
		activeSessions := 0
		for _, conn := range g.connections {
			if conn.Config.ID == connID {
				conn.mu.Lock()
				activeSessions = int(conn.sessions)
				conn.mu.Unlock()
				break
			}
		}

		report := MetricsReport{
			ConnectionID:   connID,
			RequestCount:   m.RequestCount,
			ErrorCount:     m.ErrorCount,
			AuthFailures:   m.AuthFailures,
			P95LatencyMs:   p95,
			ActiveSessions: activeSessions,
		}
		if !m.LastRequestAt.IsZero() {
			report.LastRequestAt = m.LastRequestAt.Format(time.RFC3339)
		}
		reports = append(reports, report)

		// Reset deltas
		m.RequestCount = 0
		m.ErrorCount = 0
		m.AuthFailures = 0
		m.Latencies = m.Latencies[:0]
	}

	return reports
}
