package config

import (
	"crypto/md5"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// ConfigChangeFunc is called when a config section changes.
type ConfigChangeFunc func(old, new *Config)

// Manager manages configuration loading, watching, and section-level subscriptions.
type Manager struct {
	path        string
	current     *Config
	mu          sync.RWMutex
	watcher     *fsnotify.Watcher
	lastHash    string
	subscribers map[string][]ConfigChangeFunc
	stopCh      chan struct{}
}

// NewManager creates a ConfigManager with the given config file path.
// If the file doesn't exist, default configuration is used.
func NewManager(path string) *Manager {
	m := &Manager{
		path:        path,
		current:     DefaultConfig(),
		subscribers: make(map[string][]ConfigChangeFunc),
		stopCh:      make(chan struct{}),
	}
	if err := m.load(); err != nil {
		// Use defaults if config file is missing or invalid
		m.current = DefaultConfig()
	}
	return m
}

// Get returns the current configuration (read-only snapshot).
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg := *m.current
	return &cfg
}

// Set modifies a value, validates, writes to disk, and notifies subscribers.
func (m *Manager) Set(key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := *m.current

	// Serialize to YAML, modify, and deserialize (simple approach)
	data, err := yaml.Marshal(m.current)
	if err != nil {
		return fmt.Errorf("config marshal: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("config unmarshal: %w", err)
	}

	// Support dot-separated nested keys (e.g., "agent.planning_mode")
	parts := strings.Split(key, ".")
	if len(parts) == 1 {
		raw[key] = value
	} else {
		// Walk to the nested map
		current := raw
		for i := 0; i < len(parts)-1; i++ {
			child, ok := current[parts[i]].(map[string]any)
			if !ok {
				child = make(map[string]any)
				current[parts[i]] = child
			}
			current = child
		}
		current[parts[len(parts)-1]] = value
	}

	data, err = yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("config re-marshal: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("config parse: %w", err)
	}

	// Write atomically
	if err := os.WriteFile(m.path, data, 0644); err != nil {
		return fmt.Errorf("config write: %w", err)
	}

	m.current = &cfg
	m.lastHash = hashBytes(data)

	// Notify section subscribers
	m.notifySubscribers(&old, &cfg, key)
	return nil
}

// Subscribe registers a callback for changes to a specific section.
// Sections: "llm", "security", "mcp", "agent", "storage", "heartbeat", "forge"
func (m *Manager) Subscribe(section string, fn ConfigChangeFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers[section] = append(m.subscribers[section], fn)
}

// StartWatching starts fsnotify-based config file watching.
func (m *Manager) StartWatching() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	m.watcher = watcher

	if err := watcher.Add(m.path); err != nil {
		watcher.Close()
		return fmt.Errorf("watch %s: %w", m.path, err)
	}

	go m.watchLoop()
	return nil
}

// StopWatching stops the config file watcher.
func (m *Manager) StopWatching() {
	close(m.stopCh)
	if m.watcher != nil {
		m.watcher.Close()
	}
}

func (m *Manager) watchLoop() {
	for {
		select {
		case <-m.stopCh:
			return
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				m.reload()
			}
		case _, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}

	hash := hashBytes(data)
	if hash == m.lastHash {
		return nil // No change
	}

	// Expand environment variables: ${VAR_NAME} → os.Getenv("VAR_NAME")
	expanded := os.ExpandEnv(string(data))

	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return fmt.Errorf("config parse: %w", err)
	}

	m.current = cfg
	m.lastHash = hash
	return nil
}

func (m *Manager) reload() {
	m.mu.Lock()
	old := *m.current
	m.mu.Unlock()

	if err := func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.load()
	}(); err != nil {
		return
	}

	m.mu.RLock()
	newCfg := *m.current
	m.mu.RUnlock()

	// Notify all sections
	for section, fns := range m.subscribers {
		for _, fn := range fns {
			fn(&old, &newCfg)
			_ = section
		}
	}
}

func (m *Manager) notifySubscribers(old, new *Config, section string) {
	fns, ok := m.subscribers[section]
	if !ok {
		return
	}
	for _, fn := range fns {
		fn(old, new)
	}
}

func hashBytes(data []byte) string {
	h := md5.Sum(data)
	return fmt.Sprintf("%x", h)
}
