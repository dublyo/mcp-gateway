package profiles

// Tool describes an MCP tool
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
}

// Profile is implemented by each MCP profile (filesystem, fetch, etc.)
type Profile interface {
	ID() string
	Tools() []Tool
	CallTool(name string, args map[string]interface{}, env map[string]string) (string, error)
}

// Registry holds all available profiles
var Registry = map[string]Profile{}

func init() {
	reg := []Profile{
		&TimeProfile{},
		&FetchProfile{},
		&MemoryProfile{},
		&FilesystemProfile{},
		&WordPressKnowledgeProfile{},
		&FilesKnowledgeProfile{},
		&ThinkingProfile{},
		&DnsProfile{},
		&CryptoProfile{},
		&HealthcheckProfile{},
		&CronProfile{},
		&RegexProfile{},
		&MathProfile{},
		&IpProfile{},
		&WebhookProfile{},
		&EmailProfile{},
		&TransformProfile{},
		&DatabaseProfile{},
		&RedisProfile{},
		&QRCodeProfile{},
		&GitProfile{},
		&DockerProfile{},
	}
	for _, p := range reg {
		Registry[p.ID()] = p
	}
}

// Get returns a profile by ID
func Get(id string) (Profile, bool) {
	p, ok := Registry[id]
	return p, ok
}
