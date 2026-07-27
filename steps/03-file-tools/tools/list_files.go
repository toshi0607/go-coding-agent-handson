package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// maxListEntries は list_files が返すエントリ数の上限。
// 巨大リポジトリで結果がコンテキストを食い潰すのを防ぐ。
const maxListEntries = 500

// ListFiles はディレクトリ配下のファイル一覧を返すツール。
// read_file と組み合わせることで「探して → 読む」ができるようになる。
type ListFiles struct {
	workDir string
}

// NewListFiles は workDir を基準にパスを解決する list_files を作る。
func NewListFiles(workDir string) *ListFiles { return &ListFiles{workDir: workDir} }

func (*ListFiles) Name() string { return "list_files" }

func (*ListFiles) Description() string {
	return "指定したディレクトリ配下のファイルとディレクトリを再帰的に一覧します。ディレクトリは末尾に / が付きます。どんなファイルがあるか把握したいときに最初に使ってください。"
}

func (*ListFiles) InputSchema() Schema {
	return Schema{
		Properties: map[string]any{
			"path": StringProperty("一覧するディレクトリの相対パス。省略時は作業ディレクトリ全体。"),
		},
	}
}

func (l *ListFiles) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("入力のパースに失敗: %w", err)
	}
	root := filepath.Join(l.workDir, in.Path)

	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// 隠しディレクトリ(.git など)の中身はノイズなのでスキップする。
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel += "/"
		}
		entries = append(entries, rel)
		if len(entries) >= maxListEntries {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "(ファイルがありません)", nil
	}
	result := strings.Join(entries, "\n")
	if len(entries) >= maxListEntries {
		result += fmt.Sprintf("\n(%d件で打ち切りました)", maxListEntries)
	}
	return result, nil
}
