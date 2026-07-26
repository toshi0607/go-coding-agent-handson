package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// bashTimeout は1コマンドの実行時間上限。
// LLMが対話的なコマンド(エディタ起動など)を実行してしまったときに
// エージェント全体が固まるのを防ぐ。
const bashTimeout = 60 * time.Second

// Bash は任意のシェルコマンドを実行するツール。
// テスト実行・ビルド・grep など、専用ツールを作っていない操作を
// 全部カバーできる万能ツールである一方、エージェントの中で最も危険な
// ツールでもある(rm -rf も curl | sh も「コマンド」だ)。
//
// 注意: このツール自体は権限チェックをしない。承認フローは
// エージェントループ側(ツール実行の直前)に置いてある。
// 「チェックをどこに差し込むか」は設計上の重要な判断で、
// ツール内に置くと個々のツール実装者の裁量に任されてしまうが、
// ループ側に置けばすべてのツールに漏れなく適用できる。
type Bash struct{}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	return "シェルコマンドを bash -c で実行し、標準出力と標準エラー出力を返します。" +
		"ビルド・テスト・検索など、他のツールでできない操作に使ってください。" +
		"対話的なコマンドは使えません。タイムアウトは60秒です。"
}

func (Bash) InputSchema() Schema {
	return Schema{
		Properties: map[string]any{
			"command": StringProperty("実行するシェルコマンド。例: go test ./..."),
		},
		Required: []string{"command"},
	}
}

func (Bash) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("入力のパースに失敗: %w", err)
	}
	if in.Command == "" {
		return "", errors.New("command を指定してください")
	}

	ctx, cancel := context.WithTimeout(ctx, bashTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", in.Command)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("タイムアウト(%s)しました:\n%s", bashTimeout, output)
	}
	if err != nil {
		// 非ゼロ終了はエラーとして返すが、出力も含める。
		// LLMはエラー出力を読んで次の一手を考えるので、出力こそが本体。
		return "", fmt.Errorf("コマンドが失敗しました (%v):\n%s", err, output)
	}
	if len(output) == 0 {
		return "(出力なし)", nil
	}
	return string(output), nil
}
