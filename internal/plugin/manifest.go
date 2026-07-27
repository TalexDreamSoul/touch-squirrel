package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// HostAPI is the contract version this host understands.
const HostAPI = "0.1"

// Kind identifies a plugin role.
type Kind string

const (
	KindRegistrar  Kind = "registrar"
	KindPoolProxy  Kind = "pool-proxy"
	KindExporter   Kind = "exporter"
	KindCapability Kind = "capability"
)

// Runtime selects how the host loads the plugin body.
type Runtime string

const (
	RuntimeGo     Runtime = "go"
	RuntimeJS     Runtime = "js"
	RuntimeHybrid Runtime = "hybrid"
	RuntimeBridge Runtime = "bridge"
)

// Entry points for go and/or js runners.
type Entry struct {
	Go     string `json:"go,omitempty"`
	JS     string `json:"js,omitempty"`
	Bridge string `json:"bridge,omitempty"`
}

// UI declares optional panel slots contributed by the plugin.
type UI struct {
	Panels []string `json:"panels,omitempty"`
	Routes []string `json:"routes,omitempty"`
}

// Manifest is plugin.json.
type Manifest struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Description   string          `json:"description,omitempty"`
	Kind          []Kind          `json:"kind"`
	Runtime       Runtime         `json:"runtime"`
	Entry         Entry           `json:"entry"`
	HostAPI       string          `json:"hostApi"`
	Capabilities  []string        `json:"capabilities,omitempty"`
	ArtifactKinds []string        `json:"artifactKinds,omitempty"`
	ConfigSchema  json.RawMessage `json:"configSchema,omitempty"`
	UI            *UI             `json:"ui,omitempty"`
	Source        string          `json:"source,omitempty"`
	Status        string          `json:"status,omitempty"`
	// Path is filled by the manager (absolute plugin root).
	Path string `json:"-"`
}

// LoadManifest reads and validates plugin.json from path.
func LoadManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("plugin.json: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Validate checks required manifest fields.
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("plugin id required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("plugin %s: version required", m.ID)
	}
	if len(m.Kind) == 0 {
		return fmt.Errorf("plugin %s: kind required", m.ID)
	}
	for _, k := range m.Kind {
		switch k {
		case KindRegistrar, KindPoolProxy, KindExporter, KindCapability:
		default:
			return fmt.Errorf("plugin %s: unknown kind %q", m.ID, k)
		}
	}
	switch m.Runtime {
	case RuntimeGo, RuntimeJS, RuntimeHybrid:
	default:
		return fmt.Errorf("plugin %s: runtime must be go|js|hybrid", m.ID)
	}
	if strings.TrimSpace(m.Entry.Go) == "" && strings.TrimSpace(m.Entry.JS) == "" {
		return fmt.Errorf("plugin %s: entry.go or entry.js required", m.ID)
	}
	if strings.TrimSpace(m.HostAPI) == "" {
		return fmt.Errorf("plugin %s: hostApi required", m.ID)
	}
	return nil
}

// HasKind reports whether the plugin declares k.
func (m Manifest) HasKind(k Kind) bool {
	for _, x := range m.Kind {
		if x == k {
			return true
		}
	}
	return false
}
