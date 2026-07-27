package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toshi0607/go-coding-agent-handson/final/hooks"
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

// 起動時の信頼確認でユーザーに見せる一覧。
// ここに漏れがあると「確認したはずのものが全部ではなかった」ことになり、
// 確認そのものが意味を失う。
func TestExecutables(t *testing.T) {
	settings := Settings{
		BashAllowlist: []string{"ls", "go"},
		Hooks: hooks.Config{
			"PreToolUse":  {{Matcher: "bash", Command: "guard.sh"}},
			"PostToolUse": {{Matcher: "*", Command: "log.sh"}},
		},
	}
	mcpCfg := MCPConfig{MCPServers: map[string]MCPServer{
		"weather": {Command: "weather-server", Args: []string{"--port", "8080"}},
	}}

	list := Executables(settings, mcpCfg)

	// フック2件 + MCPサーバー1件 + allowlist 1件。
	if len(list) != 4 {
		t.Fatalf("実行されるものが漏れている: %+v", list)
	}

	joined := ""
	for _, e := range list {
		joined += e.Source + " / " + e.Command + "\n"
	}
	for _, want := range []string{
		"guard.sh", "log.sh",
		"weather-server --port 8080", // 引数まで見せないと判断できない
		"ls, go",                     // allowlist も「承認を省く」という判断
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("一覧に %q がない:\n%s", want, joined)
		}
	}

	// mapの反復順に左右されず、表示が毎回同じ順になること。
	for i := range 5 {
		again := Executables(settings, mcpCfg)
		if !slices.Equal(again, list) {
			t.Fatalf("%d回目の順序が違う: %+v", i, again)
		}
	}
}

// 何も実行されない設定なら一覧は空(= 確認プロンプトを出さない)。
func TestExecutablesEmpty(t *testing.T) {
	if list := Executables(Settings{}, MCPConfig{}); len(list) != 0 {
		t.Errorf("空のはず: %+v", list)
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
