package language

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// LangBasic represents basic language information
type LangBasic struct {
	Name   string `toml:"name"`
	ID     int    `toml:"id"`
	Suffix string `toml:"suffix"`
}

// LangConfig represents complete language configuration
type LangConfig struct {
	Name string  `toml:"name"`
	Fs   FsInfo  `toml:"fs"`
	Cmd  CmdInfo `toml:"cmd"`
}

// FsInfo represents filesystem information for a language
type FsInfo struct {
	Base    string `toml:"base"`
	Workdir string `toml:"workdir"`
}

// CmdInfo represents command information for a language
type CmdInfo struct {
	Compile string   `toml:"compile"`
	Run     string   `toml:"run"`
	Ver     string   `toml:"ver"`
	Env     []string `toml:"env"`
}

// Manager manages language configurations
type Manager struct {
	langMap map[int]LangBasic
	homeDir string
}

// NewLanguageManager creates a new language manager
func NewLanguageManager(homeDir string) (*Manager, error) {
	manager := &Manager{
		homeDir: homeDir,
		langMap: make(map[int]LangBasic),
	}

	if err := manager.loadLanguageMap(); err != nil {
		return nil, fmt.Errorf("failed to load language map: %w", err)
	}

	return manager, nil
}

// loadLanguageMap loads the basic language mapping from all.toml
func (m *Manager) loadLanguageMap() error {
	data, err := os.ReadFile(filepath.Join(m.homeDir, "etc", "langs", "all.toml"))
	if err != nil {
		return fmt.Errorf("failed to read all.toml: %w", err)
	}

	var config struct {
		Lang []LangBasic `toml:"lang"`
	}

	if err := toml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse all.toml: %w", err)
	}

	for _, lang := range config.Lang {
		m.langMap[lang.ID] = lang
	}

	return nil
}

// GetLanguageBasic returns basic language information
func (m *Manager) GetLanguageBasic(langID int) (LangBasic, error) {
	lang, ok := m.langMap[langID]
	if !ok {
		return LangBasic{}, fmt.Errorf("unknown language ID: %d", langID)
	}
	return lang, nil
}

// GetLanguageConfig returns complete language configuration
func (m *Manager) GetLanguageConfig(langID int) (*LangConfig, error) {
	langPath := filepath.Join(m.homeDir, "etc", "langs", fmt.Sprintf("%d.lang.toml", langID))

	data, err := os.ReadFile(langPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read language config: %w", err)
	}

	var config LangConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse language config: %w", err)
	}

	return &config, nil
}

// GetAllLanguages returns all available languages
func (m *Manager) GetAllLanguages() map[int]LangBasic {
	result := make(map[int]LangBasic)
	for k, v := range m.langMap {
		result[k] = v
	}
	return result
}
