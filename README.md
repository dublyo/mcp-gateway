# Dublyo MCP Gateway

A lightweight, multi-tenant [Model Context Protocol](https://modelcontextprotocol.io) (MCP) gateway that runs on any server and exposes MCP tools over HTTPS. Built in Go with zero framework dependencies.

Each gateway instance hosts multiple MCP connections — each with its own subdomain, API key, and profile — all routed through a single container.

## Features

- **18 built-in profiles** — Filesystem, Web Fetch, WordPress Knowledge, Memory, Time, DNS, Crypto, Healthcheck, Cron, Regex, Math, IP, Webhook, Email, Data Transform, Database (PostgreSQL), Redis, and Sequential Thinking
- **Two MCP transports** — SSE (Claude Desktop compatible) and Streamable HTTP
- **Per-connection auth** — Peppered SHA-256 API key verification with constant-time comparison
- **Rate limiting** — Sliding window per connection (configurable requests/minute)
- **Concurrency control** — Max concurrent sessions per connection
- **Metrics reporting** — Request counts, error rates, P95 latency, active sessions
- **Auto-config sync** — Polls the Dublyo API every 30s for connection changes
- **Auto token refresh** — Gateway JWT tokens refresh transparently before expiry
- **Traefik integration** — Docker labels for wildcard subdomain routing
- **Tiny footprint** — ~15MB Alpine-based Docker image

## Quick Start

```bash
docker run -d \
  --name mcp-gateway \
  -e GATEWAY_TOKEN=your_jwt_token \
  -e DUBLYO_API_URL=https://your-api-url.example.com \
  -p 8080:8080 \
  ghcr.io/dublyo/mcp-gateway:latest
```

Or with Docker Compose:

```yaml
services:
  mcp-gateway:
    image: ghcr.io/dublyo/mcp-gateway:latest
    restart: unless-stopped
    environment:
      - GATEWAY_TOKEN=${GATEWAY_TOKEN}
      - DUBLYO_API_URL=https://your-api-url.example.com
    ports:
      - "8080:8080"
    volumes:
      - mcp-data:/data

volumes:
  mcp-data:
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GATEWAY_TOKEN` | Yes | — | JWT token for Dublyo API authentication |
| `GATEWAY_PORT` | No | `8080` | HTTP listen port |
| `DUBLYO_API_URL` | No | — | Dublyo API base URL for config sync and metrics |
| `SYNC_INTERVAL` | No | `30s` | Config sync polling interval |
| `METRICS_INTERVAL` | No | `30s` | Metrics reporting interval |
| `LOG_LEVEL` | No | `info` | Log verbosity (`debug`, `info`, `warn`, `error`) |

## Architecture

```
Client (Claude Desktop / SDK / Any MCP Client)
    │
    │  HTTPS + Bearer Token
    ▼
┌─────────────────────────────────────────┐
│  MCP Gateway Container (:8080)          │
│                                         │
│  Host header ──► Connection lookup      │
│  Bearer token ──► SHA-256 verification  │
│  Rate limit check ──► Sliding window    │
│                                         │
│  ┌─────────┐  ┌─────────┐              │
│  │ SSE     │  │ HTTP    │  Transports   │
│  │ /sse    │  │ /mcp    │              │
│  └────┬────┘  └────┬────┘              │
│       └──────┬─────┘                    │
│              ▼                          │
│  ┌──────────────────┐                   │
│  │ JSON-RPC Handler │                   │
│  └────────┬─────────┘                   │
│           ▼                             │
│  ┌──────────────────┐                   │
│  │ Profile Handler  │  (18 profiles)    │
│  └──────────────────┘                   │
└─────────────────────────────────────────┘
         │                    ▲
         │ Metrics (30s)      │ Config sync (30s)
         ▼                    │
    Dublyo API ───────────────┘
```

## Endpoints

| Route | Method | Auth | Description |
|-------|--------|------|-------------|
| `/health` | GET | None | Health check — returns `{"status":"ok"}` |
| `/sse` | GET | Bearer | Opens SSE stream (Claude Desktop compatible) |
| `/message` | POST | Bearer | Sends JSON-RPC message to SSE session |
| `/mcp` | POST | Bearer | Streamable HTTP — JSON-RPC request/response |
| `/mcp` | GET | Bearer | Streamable HTTP — keep-alive SSE stream |
| `/mcp` | DELETE | None | Streamable HTTP — terminate session |

## Profiles

| ID | Name | Tools | Requires Config |
|----|------|-------|-----------------|
| `filesystem` | Filesystem | 8 | `ALLOWED_PATHS` |
| `fetch` | Web Fetch | 2 | Optional `ALLOWED_DOMAINS` |
| `wordpress-knowledge` | WordPress Knowledge | 4 | `LLMS_TXT_URL` |
| `memory` | Memory | 5 | Optional `PERSIST_PATH` |
| `time` | Time & Timezone | 4 | Optional `DEFAULT_TIMEZONE` |
| `thinking` | Sequential Thinking | 1 | None |
| `dns` | DNS & Network | 4 | None |
| `crypto` | Hash & Crypto | 6 | None |
| `healthcheck` | HTTP & SSL Monitor | 4 | None |
| `cron` | Cron Scheduler | 3 | None |
| `regex` | Regex Tester | 4 | None |
| `math` | Math & Calculator | 5 | None |
| `ip` | IP & Networking | 4 | None |
| `webhook` | Webhook Sender | 3 | Optional `SLACK_WEBHOOK_URL`, `DISCORD_WEBHOOK_URL` |
| `email` | Email Sender | 3 | `SMTP_HOST`, `FROM_ADDRESS` |
| `transform` | Data Transform | 8 | None |
| `database` | Database (PostgreSQL) | 4 | `DATABASE_URL` |
| `redis` | Redis | 6 | `REDIS_URL` |

## Adding a Profile

1. Create `internal/profiles/yourprofile.go` implementing the `Profile` interface:

```go
package profiles

type YourProfile struct{}

func (p *YourProfile) ID() string { return "your-profile" }

func (p *YourProfile) Tools() []Tool {
    return []Tool{
        {
            Name:        "your_tool",
            Description: "What your tool does",
            InputSchema: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "param": map[string]interface{}{
                        "type":        "string",
                        "description": "Parameter description",
                    },
                },
                "required": []string{"param"},
            },
        },
    }
}

func (p *YourProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
    switch name {
    case "your_tool":
        // Implementation here
        return "result", nil
    default:
        return "", fmt.Errorf("unknown tool: %s", name)
    }
}
```

2. Register it in `internal/profiles/profiles.go`:

```go
func init() {
    reg := []Profile{
        // ... existing profiles
        &YourProfile{},
    }
    // ...
}
```

3. Push to `main` — CI builds and pushes to GHCR automatically.

## Building from Source

```bash
# Clone
git clone https://github.com/dublyo/mcp-gateway.git
cd mcp-gateway

# Build binary
go build -o mcp-gateway ./cmd/gateway

# Run
GATEWAY_TOKEN=your_token ./mcp-gateway
```

### Docker Build

```bash
docker build -t mcp-gateway .
docker run -e GATEWAY_TOKEN=your_token -p 8080:8080 mcp-gateway
```

## MCP Protocol

The gateway implements the [Model Context Protocol](https://modelcontextprotocol.io) specification version `2024-11-05`.

**Supported JSON-RPC methods:**
- `initialize` — Returns server info and capabilities
- `ping` — Health check
- `tools/list` — Returns available tools for the connection's profile
- `tools/call` — Executes a tool and returns results

**Connecting with Claude Desktop:**

Add to your Claude Desktop config (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "my-server": {
      "url": "https://my-server-a1b2c3d4.dublyo.xyz/sse",
      "headers": {
        "Authorization": "Bearer dky_your_api_key_here"
      }
    }
  }
}
```

## Project Structure

```
mcp-gateway/
├── cmd/gateway/main.go           # Entry point
├── internal/
│   ├── gateway/
│   │   ├── gateway.go            # Core: connections, auth, rate limits, metrics
│   │   ├── poller.go             # Config sync + metrics reporting loops
│   │   └── traefik.go            # Optional Traefik file provider config
│   ├── mcp/
│   │   ├── handler.go            # JSON-RPC 2.0 protocol handler
│   │   └── types.go              # MCP protocol type definitions
│   ├── profiles/
│   │   ├── profiles.go           # Profile interface + registry
│   │   ├── filesystem.go         # File operations (sandboxed)
│   │   ├── fetch.go              # HTTP fetch (SSRF-safe)
│   │   ├── wordpress_knowledge.go # WordPress llms.txt search
│   │   ├── memory.go             # Key-value store
│   │   ├── time.go               # Timezone operations
│   │   ├── thinking.go           # Structured reasoning
│   │   ├── dns.go                # DNS lookups
│   │   ├── crypto.go             # Hashing, UUID, passwords
│   │   ├── healthcheck.go        # URL/SSL monitoring
│   │   ├── cron.go               # Cron expressions
│   │   ├── regex.go              # Regex operations
│   │   ├── math_profile.go       # Math/stats/conversion
│   │   ├── ip.go                 # IP/CIDR/subnet
│   │   ├── webhook.go            # Webhooks, Slack, Discord
│   │   ├── email.go              # SMTP email
│   │   ├── transform.go          # JSON, Base64, URL encoding
│   │   ├── database.go           # PostgreSQL queries
│   │   └── redis.go              # Redis operations
│   └── server/
│       └── server.go             # HTTP server, SSE + HTTP transports
├── Dockerfile                    # Multi-stage build (Alpine 3.20)
├── go.mod
└── .github/workflows/build.yml   # CI: build + push to GHCR
```

## License

This project is licensed under the [GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0).

You are free to use, modify, and distribute this software. If you modify it and run it as a network service, you must make your modified source code available to users of that service.

Built by [Dublyo](https://dublyo.com).
