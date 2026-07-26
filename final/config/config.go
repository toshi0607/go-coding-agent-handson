// Package config は設定ファイルの読み込みを担う。
//
// このエージェントは作業ディレクトリの次のファイルを読む:
//   - CLAUDE.md              プロジェクトメモリ(prompt パッケージが読む)
//   - .agent/settings.json   bashのallowlistとhooks(このパッケージ)
//   - .agent/commands/*.md   カスタムコマンド(command パッケージが読む)
//   - .mcp.json              MCPサーバー定義(このパッケージ)
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/toshi0607/go-coding-agent-handson/final/hooks"
)

// SettingsFile は設定ファイルのパス(作業ディレクトリ基準)。
const SettingsFile = ".agent/settings.json"

// MCPFile はMCPサーバー定義ファイルのパス(作業ディレクトリ基準)。
const MCPFile = ".mcp.json"

// Settings は .agent/settings.json の内容。
type Settings struct {
	// BashAllowlist は承認なしで実行を許可するコマンド名(先頭単語)。
	BashAllowlist []string `json:"bashAllowlist"`
	// Hooks はツール実行前後に走らせる外部コマンド。
	Hooks hooks.Config `json:"hooks"`
}

// MCPServer は1つのMCPサーバーの起動方法。
type MCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// MCPConfig は .mcp.json の内容。
type MCPConfig struct {
	MCPServers map[string]MCPServer `json:"mcpServers"`
}

// LoadSettings は設定を読む。ファイルがなければゼロ値を返す(設定は任意)。
func LoadSettings(workDir string) (Settings, error) {
	var s Settings
	if err := loadJSON(filepath.Join(workDir, SettingsFile), &s); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// LoadMCPConfig はMCPサーバー定義を読む。ファイルがなければゼロ値を返す。
func LoadMCPConfig(workDir string) (MCPConfig, error) {
	var c MCPConfig
	if err := loadJSON(filepath.Join(workDir, MCPFile), &c); err != nil {
		return MCPConfig{}, err
	}
	return c, nil
}

func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s のパースに失敗: %w", path, err)
	}
	return nil
}
