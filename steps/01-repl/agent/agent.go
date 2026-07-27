// Package agent はエージェントの本体を実装する。
//
// この段階のエージェントはまだツールを持たない、ただの対話ループである。
// それでも「会話の記憶はどこにあるのか」という最初の問いに答えるには
// 十分な材料が揃っている。
//
// Messages APIはステートレスである。APIサーバーは前回のやりとりを
// 覚えていないので、会話を続けるには、こちらが履歴全体を毎回
// 送り直す必要がある。「会話の記憶」の実体は、サーバー側ではなく
// 手元の history スライスにある。
package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/01-repl/llm"
)

// maxTokensPerTurn は1回のLLM呼び出しの出力上限。
const maxTokensPerTurn = 8192

// Config はエージェントの構成。
type Config struct {
	Client llm.Client
	Model  anthropic.Model
	// Out は応答テキストの出力先。
	Out io.Writer
}

// Agent は会話履歴を持つエージェント。
type Agent struct {
	client llm.Client
	model  anthropic.Model
	out    io.Writer

	// history は会話履歴。Messages APIはステートレスなので、
	// 毎回の呼び出しで履歴全体を送り直す。「会話の記憶」の実体は
	// サーバー側ではなく、このスライスにある。
	history []anthropic.MessageParam
}

// New はエージェントを作る。
func New(cfg Config) *Agent {
	out := cfg.Out
	if out == nil {
		out = io.Discard
	}
	return &Agent{client: cfg.Client, model: cfg.Model, out: out}
}

// Run は1ユーザーターンを処理する。ユーザー入力を履歴に積んでLLMを呼び、
// 応答を表示し、応答も履歴に積んで返す。
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	// ユーザー入力を user メッセージとして履歴に積む。
	a.history = append(a.history, anthropic.NewUserMessage(anthropic.NewTextBlock(userInput)))

	resp, err := a.client.Complete(ctx, a.buildParams())
	if err != nil {
		return "", fmt.Errorf("LLM呼び出しに失敗: %w", err)
	}
	fmt.Fprintln(a.out, textOf(resp))

	// アシスタントの応答も履歴に積む。これを忘れると、次のターンの
	// リクエストに「LLM自身の前回の発言」が含まれず、LLMは自分が
	// 何を答えたか知らないまま会話を続けることになる。
	a.history = append(a.history, resp.ToParam())

	return textOf(resp), nil
}

// buildParams はAPIリクエストを組み立てる。
func (a *Agent) buildParams() anthropic.MessageNewParams {
	return anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: maxTokensPerTurn,
		Messages:  a.history,
	}
}

// History は現在の会話履歴を返す(テスト・デバッグ用)。
func (a *Agent) History() []anthropic.MessageParam {
	return a.history
}

// textOf はレスポンスからテキストブロックを連結して返す。
func textOf(msg *anthropic.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}
