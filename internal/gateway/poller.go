package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Poller handles config sync and metrics reporting to the Dublyo API
type Poller struct {
	gateway      *Gateway
	apiURL       string
	token        string
	syncInterval time.Duration
	httpClient   *http.Client
	failures     int
	traefikDir   string
}

func NewPoller(gw *Gateway) *Poller {
	apiURL := os.Getenv("DUBLYO_API_URL")
	if apiURL == "" {
		apiURL = "https://api26.dublyo.com"
	}

	syncInterval := 30 * time.Second
	if s := os.Getenv("SYNC_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			syncInterval = d
		}
	}

	traefikDir := os.Getenv("TRAEFIK_DYNAMIC_DIR")
	if traefikDir == "" {
		traefikDir = "/traefik-dynamic"
	}

	return &Poller{
		gateway:      gw,
		apiURL:       apiURL,
		token:        os.Getenv("GATEWAY_TOKEN"),
		syncInterval: syncInterval,
		traefikDir:   traefikDir,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Start runs the config sync and metrics loops
func (p *Poller) Start(ctx context.Context) {
	// Initial sync
	p.syncConfig()

	// Config sync ticker
	syncTicker := time.NewTicker(p.syncInterval)
	defer syncTicker.Stop()

	// Metrics ticker (offset by half the sync interval)
	metricsDelay := p.syncInterval / 2
	time.Sleep(metricsDelay)
	metricsTicker := time.NewTicker(p.syncInterval)
	defer metricsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-syncTicker.C:
			p.syncConfig()
		case <-metricsTicker.C:
			p.reportMetrics()
		}
	}
}

func (p *Poller) syncConfig() {
	url := fmt.Sprintf("%s/internal/gateway/sync", p.apiURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("[poller] sync request error: %v", err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("X-Config-Version", strconv.FormatInt(p.gateway.Version(), 10))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.failures++
		if p.failures >= 5 {
			log.Printf("[poller] sync failed %d consecutive times", p.failures)
		}
		return
	}
	defer resp.Body.Close()

	// Check for token refresh
	if newToken := resp.Header.Get("X-Gateway-Token"); newToken != "" {
		p.token = newToken
		log.Printf("[poller] gateway token refreshed")
	}

	switch resp.StatusCode {
	case http.StatusNotModified:
		p.failures = 0
		return
	case http.StatusOK:
		p.failures = 0
	case http.StatusUnauthorized, http.StatusForbidden:
		log.Printf("[poller] auth failed (status %d) — token may be revoked", resp.StatusCode)
		p.failures++
		return
	default:
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[poller] sync unexpected status %d: %s", resp.StatusCode, string(body))
		p.failures++
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[poller] read body error: %v", err)
		return
	}

	var apiResp struct {
		Success bool          `json:"success"`
		Data    GatewayConfig `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		log.Printf("[poller] decode error: %v", err)
		return
	}

	if !apiResp.Success {
		log.Printf("[poller] API returned success=false")
		return
	}

	// Apply new config
	p.gateway.ApplyConfig(apiResp.Data)

	// Generate Traefik dynamic config (optional — skip if dir is empty or not configured)
	if p.traefikDir != "" {
		if err := GenerateTraefikConfig(p.traefikDir, apiResp.Data.Connections); err != nil {
			log.Printf("[poller] traefik config generation failed: %v", err)
		}
	}
}

func (p *Poller) reportMetrics() {
	reports := p.gateway.CollectAndResetMetrics()
	if len(reports) == 0 {
		return
	}

	payload := map[string]interface{}{
		"metrics": reports,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	url := fmt.Sprintf("%s/internal/gateway/metrics", p.apiURL)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return
	}

	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		log.Printf("[poller] metrics report failed: %v", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("[poller] metrics report status %d", resp.StatusCode)
	}
}
