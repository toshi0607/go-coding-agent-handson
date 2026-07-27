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
//
// パスは Workspace 経由で必ず検証する。読み取りは「安全な操作」に
// 見えるが、作業ディレクトリの外を読めるなら ~/.ssh/id_rsa も
// .env も読めてしまい、その中身はAPIリクエストに載って外に出る。
// 読み取りの危険は「壊すこと」ではなく「漏らすこと」にある。
type ReadFile struct {
	ws *Workspace
}

// NewReadFile は作業ディレクトリに封じ込められた read_file を作る。
func NewReadFile(ws *Workspace) *ReadFile { return &ReadFile{ws: ws} }

func (*ReadFile) Name() string { return "read_file" }

func (*ReadFile) Description() string {
	return "指定した相対パスのファイルの中身を返します。ファイルの内容を確認したいときに使ってください。作業ディレクトリの外のファイルは読めません。"
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
	path, err := r.ws.Resolve(in.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// エラーメッセージはそのままLLMに返る。LLMはこれを読んで
		// パスを修正したり list_files で探し直したりできる。
		return "", err
	}
	return string(data), nil
}
