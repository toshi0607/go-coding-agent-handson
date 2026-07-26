package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// ReadFile はファイルの中身を読むツール。
// コーディングエージェントの最初の一歩。これがあるだけで
// 「コードについて質問すると、実物を読んで答える」エージェントになる。
type ReadFile struct{}

func (ReadFile) Name() string { return "read_file" }

func (ReadFile) Description() string {
	return "指定した相対パスのファイルの中身を返します。ファイルの内容を確認したいときに使ってください。ディレクトリには使えません。"
}

func (ReadFile) InputSchema() Schema {
	return Schema{
		Properties: map[string]any{
			"path": StringProperty("読みたいファイルの相対パス。例: main.go, src/util.go"),
		},
		Required: []string{"path"},
	}
}

func (ReadFile) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("入力のパースに失敗: %w", err)
	}
	data, err := os.ReadFile(in.Path)
	if err != nil {
		// エラーメッセージはそのままLLMに返る。LLMはこれを読んで
		// パスを修正したり list_files で探し直したりできる。
		return "", err
	}
	return string(data), nil
}
