// Package hooks はツール実行の前後に外部コマンドを差し込む拡張点を提供する。
//
// hooksはエージェント本体を変更せずに挙動をカスタマイズする仕組みで、
// LLMへの「お願い」(プロンプト)と違い、プログラムとして確実に実行される。
// 「◯◯を必ずチェックして」はプロンプトに書いても忘れられることがあるが、
// hookなら100%走る。この確実性の違いが、プロンプトと拡張点の使い分けの軸になる。
//
// イベントは2種類:
//   - PreToolUse:  ツール実行の直前。終了コード2で実行をブロックできる
//     (標準エラー出力がLLMへのフィードバックになる)。
//     0か2以外の終了コードは「契約違反=判定できていない」と見なし、
//     安全側に倒してブロックする(詳細は RunPre のコメント)
//   - PostToolUse: ツール実行の直後。ログ記録やフォーマッタ実行などに使う
//     (実行結果には介入できず、失敗しても無視される)
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// hookTimeout は1つのhookコマンドの実行時間上限。
const hookTimeout = 10 * time.Second

// blockExitCode はPreToolUseでツール実行をブロックする終了コード。
const blockExitCode = 2

// Hook は1つのフック定義。
type Hook struct {
	// Matcher は対象のツール名。"*" または空文字なら全ツールに適用。
	Matcher string `json:"matcher"`
	// Command は bash -c で実行するコマンド。標準入力にイベント情報の
	// JSONが渡される。
	Command string `json:"command"`
}

// Config はイベント名("PreToolUse" / "PostToolUse")→ フック定義のマップ。
// .agent/settings.json の "hooks" セクションがそのままこの形になる。
type Config map[string][]Hook

// payload はhookコマンドの標準入力に渡すJSON。
type payload struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolOutput    string          `json:"tool_output,omitempty"`
}

// Runner は設定に従ってhookを実行する。
type Runner struct {
	config Config
}

// NewRunner は Runner を作る。config は nil でもよい(何も実行しない)。
func NewRunner(config Config) *Runner {
	return &Runner{config: config}
}

// RunPre はPreToolUseフックを実行する。いずれかのhookがブロックを
// 表明したら error を返し、呼び出し側はツール実行を中止する。
func (r *Runner) RunPre(ctx context.Context, toolName string, input json.RawMessage) error {
	for _, h := range r.config["PreToolUse"] {
		if !matches(h.Matcher, toolName) {
			continue
		}
		// TODO(step10-2): フックを実行し、結果を判定する。
		//
		// イベント情報を payload に詰めて r.run(ctx, h.Command, p) で
		// 実行すると、stderr・終了コード・実行エラーが返る。
		//
		// PreToolUseフックの契約は2つの終了コードだけである:
		//   exit 0 = 許可(次のフックへ進む)
		//   exit 2 = ブロック(blockExitCode。stderr が理由。
		//            その内容をエラーに含めて返すと、LLMに理由が伝わり
		//            LLMは別の方法を試せる)
		//
		// では、それ以外はどうするか。起動失敗・タイムアウトによるkill・
		// スクリプトのバグによる exit 1 は、いずれも「契約違反 = 検査が
		// 完了していない」を意味する。ここで素通しさせると、フックが
		// 壊れた瞬間に防御が黙って無効化される。安全機構は「壊れたら
		// 止まる」(fail-closed)が正しく、「壊れたら通す」(fail-open)は
		// 最悪の設計である——という方針で判定を書くこと。
		//
		// エラーメッセージの組み立てに fmt を使うなら import も足すこと。
		panic("TODO(step10-2): steps/10-streaming-hooks/hooks/hooks.go を実装してください(hints.md 参照)")
	}
	return nil
}

// RunPost はPostToolUseフックを実行する。失敗してもツールの結果には
// 影響させない(ベストエフォート)。
func (r *Runner) RunPost(ctx context.Context, toolName string, input json.RawMessage, output string) {
	for _, h := range r.config["PostToolUse"] {
		if !matches(h.Matcher, toolName) {
			continue
		}
		p := payload{HookEventName: "PostToolUse", ToolName: toolName, ToolInput: input, ToolOutput: output}
		_, _, _ = r.run(ctx, h.Command, p)
	}
}

// run はhookコマンドを実行し、stderr と終了コードを返す。
func (r *Runner) run(ctx context.Context, command string, p payload) (stderr []byte, exitCode int, err error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, 0, err
	}

	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Stdin = bytes.NewReader(data)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		return errBuf.Bytes(), exitErr.ExitCode(), nil
	}
	return errBuf.Bytes(), 0, runErr
}

func matches(matcher, toolName string) bool {
	return matcher == "" || matcher == "*" || matcher == toolName
}
