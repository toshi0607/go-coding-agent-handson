package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReadFile はファイルの中身を読むツール。
// コーディングエージェントの最初の一歩。これがあるだけで
// 「コードについて質問すると、実物を読んで答える」エージェントになる。
//
// パスは作業ディレクトリ基準の相対パスとして解決する。
//
// 注意: この素朴な実装は "../../etc/passwd" のようなパスで
// 作業ディレクトリの外にも手が届いてしまう。何が問題で、
// どう封じ込めるかは step 04 で扱う。
type ReadFile struct {
	workDir string
}

// NewReadFile は workDir を基準にパスを解決する read_file を作る。
func NewReadFile(workDir string) *ReadFile { return &ReadFile{workDir: workDir} }

func (*ReadFile) Name() string { return "read_file" }

func (*ReadFile) Description() string {
	return "指定した相対パスのファイルの中身を返します。ファイルの内容を確認したいときに使ってください。"
}

func (*ReadFile) InputSchema() Schema {
	return Schema{
		Properties: map[string]any{
			"path": StringProperty("読みたいファイルの相対パス。例: main.go, src/util.go"),
		},
		Required: []string{"path"},
	}
}

func (r *ReadFile) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("入力のパースに失敗: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(r.workDir, in.Path))
	if err != nil {
		// エラーメッセージはそのままLLMに返る。LLMはこれを読んで
		// パスを修正したり list_files で探し直したりできる。
		return "", err
	}
	return string(data), nil
}
