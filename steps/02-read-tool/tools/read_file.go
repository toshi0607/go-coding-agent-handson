package tools

import (
	"context"
	"encoding/json"
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
	// TODO(step02-1): ツールの実行本体を実装する(ウォームアップ)。
	//
	//  1. input(LLMが生成した引数のJSON)から path を取り出す。
	//     `json:"path"` タグを付けた無名structにUnmarshalするのがGoの定石
	//  2. filepath.Join(r.workDir, path) でパスを組み立てる
	//  3. os.ReadFile で読んで中身を返す
	//
	// 読み取りに失敗したときのエラーはそのまま返してよい。エラーは
	// エージェント側で tool_result としてLLMに返され、LLMはそれを
	// 読んでパスを修正するなど、次の一手を考えられる。
	//
	// 必要な import(os, path/filepath など)は自分で足すこと。
	panic("TODO(step02-1): steps/02-read-tool/tools/read_file.go を実装してください(hints.md 参照)")
}
