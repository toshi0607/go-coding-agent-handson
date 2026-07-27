// Package config は設定ファイルの読み込みを担う。
//
// このエージェントは作業ディレクトリの次のファイルを読む:
//   - CLAUDE.md              プロジェクトメモリ(prompt パッケージが読む)
//   - .agent/settings.json   bashのallowlist(このパッケージ)
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SettingsFile は設定ファイルのパス(作業ディレクトリ基準)。
const SettingsFile = ".agent/settings.json"

// Settings は .agent/settings.json の内容。
type Settings struct {
	// BashAllowlist は承認なしで実行を許可するコマンド名(先頭単語)。
	BashAllowlist []string `json:"bashAllowlist"`
}

// LoadSettings は設定を読む。ファイルがなければゼロ値を返す(設定は任意)。
func LoadSettings(workDir string) (Settings, error) {
	var s Settings
	if err := loadJSON(filepath.Join(workDir, SettingsFile), &s); err != nil {
		return Settings{}, err
	}
	return s, nil
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
