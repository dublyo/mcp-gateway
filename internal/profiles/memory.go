package profiles

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type MemoryProfile struct{}

func (p *MemoryProfile) ID() string { return "memory" }

func (p *MemoryProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "store",
			Description: "Store a value with a key",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{
						"type":        "string",
						"description": "The key to store the value under",
					},
					"value": map[string]interface{}{
						"type":        "string",
						"description": "The value to store",
					},
				},
				"required": []string{"key", "value"},
			},
		},
		{
			Name:        "retrieve",
			Description: "Retrieve a value by key",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{
						"type":        "string",
						"description": "The key to retrieve",
					},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "list_keys",
			Description: "List all stored keys",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prefix": map[string]interface{}{
						"type":        "string",
						"description": "Optional prefix to filter keys",
					},
				},
			},
		},
		{
			Name:        "delete",
			Description: "Delete a key-value pair",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{
						"type":        "string",
						"description": "The key to delete",
					},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "clear",
			Description: "Clear all stored data",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// Per-connection memory stores, keyed by connection slug
var (
	memStores   = map[string]*memStore{}
	memStoresMu sync.Mutex
)

type memStore struct {
	mu   sync.RWMutex
	data map[string]string
	path string // persistence path (empty = in-memory only)
}

func getMemStore(env map[string]string) *memStore {
	// Use PERSIST_PATH as store identity
	path := env["PERSIST_PATH"]
	key := path
	if key == "" {
		key = "_default_"
	}

	memStoresMu.Lock()
	defer memStoresMu.Unlock()

	if s, ok := memStores[key]; ok {
		return s
	}

	s := &memStore{data: make(map[string]string), path: path}

	// Load from disk if persistence path exists
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.data)
		}
	}

	memStores[key] = s
	return s
}

func (s *memStore) persist() {
	if s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(s.path, data, 0644)
}

func (p *MemoryProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	store := getMemStore(env)
	maxEntries := 10000
	if me := env["MAX_ENTRIES"]; me != "" {
		if n, err := strconv.Atoi(me); err == nil {
			maxEntries = n
		}
	}

	switch name {
	case "store":
		key := getStr(args, "key")
		value := getStr(args, "value")
		if key == "" || value == "" {
			return "", fmt.Errorf("key and value are required")
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if _, exists := store.data[key]; !exists && len(store.data) >= maxEntries {
			return "", fmt.Errorf("maximum entries (%d) reached", maxEntries)
		}
		store.data[key] = value
		store.persist()
		return fmt.Sprintf("Stored '%s' (%d bytes)", key, len(value)), nil

	case "retrieve":
		key := getStr(args, "key")
		if key == "" {
			return "", fmt.Errorf("key is required")
		}
		store.mu.RLock()
		defer store.mu.RUnlock()
		value, ok := store.data[key]
		if !ok {
			return fmt.Sprintf("Key '%s' not found", key), nil
		}
		return value, nil

	case "list_keys":
		prefix := getStr(args, "prefix")
		store.mu.RLock()
		defer store.mu.RUnlock()
		var keys []string
		for k := range store.data {
			if prefix == "" || strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			return "No keys found", nil
		}
		return fmt.Sprintf("Keys (%d):\n%s", len(keys), strings.Join(keys, "\n")), nil

	case "delete":
		key := getStr(args, "key")
		if key == "" {
			return "", fmt.Errorf("key is required")
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if _, ok := store.data[key]; !ok {
			return fmt.Sprintf("Key '%s' not found", key), nil
		}
		delete(store.data, key)
		store.persist()
		return fmt.Sprintf("Deleted '%s'", key), nil

	case "clear":
		store.mu.Lock()
		defer store.mu.Unlock()
		count := len(store.data)
		store.data = make(map[string]string)
		store.persist()
		return fmt.Sprintf("Cleared %d entries", count), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}
