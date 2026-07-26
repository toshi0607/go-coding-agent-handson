package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
	  "bashAllowlist": ["ls", "go"],
	  "hooks": {
	    "PreToolUse": [{"matcher": "bash", "command": "exit 0"}]
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if len(s.BashAllowlist) != 2 || s.BashAllowlist[0] != "ls" {
		t.Errorf("bashAllowlist: %+v", s.BashAllowlist)
	}
	if len(s.Hooks["PreToolUse"]) != 1 {
		t.Errorf("hooks: %+v", s.Hooks)
	}
}

func TestLoadSettingsMissingFileIsOK(t *testing.T) {
	// 設定ファイルは任意。なければゼロ値で動く。
	s, err := LoadSettings(t.TempDir())
	if err != nil {
		t.Fatalf("設定ファイルなしはエラーではない: %v", err)
	}
	if len(s.BashAllowlist) != 0 {
		t.Errorf("ゼロ値のはず: %+v", s)
	}
}

func TestLoadMCPConfig(t *testing.T) {
	dir := t.TempDir()
	content := `{"mcpServers": {"weather": {"command": "weather-server", "args": ["--v"]}}}`
	if err := os.WriteFile(filepath.Join(dir, MCPFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadMCPConfig(dir)
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	server, ok := c.MCPServers["weather"]
	if !ok || server.Command != "weather-server" || len(server.Args) != 1 {
		t.Errorf("mcpServers: %+v", c.MCPServers)
	}
}
