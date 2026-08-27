// Package config は設定ファイルの読み込みを担う。
//
// このエージェントは作業ディレクトリの次のファイルを読む:
//   - CLAUDE.md              プロジェクトメモリ(prompt パッケージが読む)
//   - .agent/settings.json   bashのallowlistとhooks(このパッケージ)
//   - .agent/commands/*.md   カスタムコマンド(command パッケージが読む)
//   - .mcp.json              MCPサーバー定義(このパッケージ)
package config

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/hooks"
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

// Executable は設定ファイルが要求する外部コマンド実行を1件表す。
type Executable struct {
	// Source はどの設定がそれを要求しているか(表示用)。
	Source string
	// Command は実行される内容。
	Command string
}

// Executables は設定ファイルを信頼するかどうかをユーザーが判断するために、
// 「このディレクトリで起動すると何が実行されるか」を列挙する。
//
// 設定ファイルは実質的に実行権限そのものである。
//   - .mcp.json の command は起動と同時に子プロセスとして実行される
//   - .agent/settings.json の hooks は bash -c で実行される
//   - bashAllowlist は「確認なしで実行してよい」の宣言、つまり
//     承認フローの穴をあらかじめ開けておくものである
//
// どれもファイルを置くだけで有効になり、他人のリポジトリを clone して
// その中でエージェントを起動しただけで発火する。中身を見せずに
// 実行するわけにはいかない。
func Executables(s Settings, m MCPConfig) []Executable {
	var list []Executable
	for name, server := range m.MCPServers {
		cmd := server.Command
		if len(server.Args) > 0 {
			cmd += " " + strings.Join(server.Args, " ")
		}
		list = append(list, Executable{
			Source:  fmt.Sprintf("%s の MCPサーバー %q", MCPFile, name),
			Command: cmd,
		})
	}
	for event, hooks := range s.Hooks {
		for _, h := range hooks {
			list = append(list, Executable{
				Source:  fmt.Sprintf("%s の %s フック", SettingsFile, event),
				Command: h.Command,
			})
		}
	}
	if len(s.BashAllowlist) > 0 {
		list = append(list, Executable{
			Source:  SettingsFile + " の bashAllowlist(承認なしで実行を許可)",
			Command: strings.Join(s.BashAllowlist, ", "),
		})
	}
	// map の反復順は不定なので、表示が毎回変わらないよう並べ替える。
	slices.SortFunc(list, func(a, b Executable) int {
		return cmp.Or(
			strings.Compare(a.Source, b.Source),
			strings.Compare(a.Command, b.Command),
		)
	})
	return list
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
