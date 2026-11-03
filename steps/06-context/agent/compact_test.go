package agent

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/tools"
)

func TestShouldCompact(t *testing.T) {
	a := New(Config{CompactThreshold: 1000})
	a.lastUsageTokens = 999
	if a.shouldCompact() {
		t.Error("閾値未満でcompactionすべきでない")
	}
	a.lastUsageTokens = 1001
	if !a.shouldCompact() {
		t.Error("閾値超過でcompactionすべき")
	}
}

func TestCompact(t *testing.T) {
	client := llm.NewReplayClient(
		llm.TextResponse("ユーザーはREPLの実装を依頼。main.goに実装済み。次はテストを書く。"),
	)
	a := New(Config{
		Client: client, Model: "claude-opus-5-5",
		SystemPrompt: "あなたはエージェントです",
	})

	history := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("REPLを実装して")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("実装しました")),
	}
	a.history = history
	if err := a.Compact(t.Context()); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// 履歴は要約1メッセージに置き換わる。
	if n := len(a.History()); n != 1 {
		t.Fatalf("圧縮後の履歴は1件のはず: %d件", n)
	}

	// 要約リクエストには元の履歴全体+要約指示が含まれる。
	reqs := client.Requests()
	if len(reqs) != 1 {
		t.Fatalf("LLM呼び出しは1回のはず: %d回", len(reqs))
	}
	if got := len(reqs[0].Messages); got != 3 {
		t.Errorf("要約リクエストは履歴2件+指示1件の3件のはず: %d件", got)
	}

	// 圧縮前の履歴スライスを壊していないこと。
	if len(history) != 2 {
		t.Errorf("元の履歴が変更された: %d件", len(history))
	}
}

// 要約リクエストが「通常のターンと同じ形」であることの回帰テスト。
//
// 履歴に tool_use / tool_result が含まれた状態で tools なしのリクエストを
// 送るとAPIに拒否され、compactionが必要な長い会話ほど失敗する——という
// 質の悪いバグになる。ReplayClientはリクエストを検証しないので、
// テスト側で明示的に確認する。
func TestCompactKeepsToolsAndSystem(t *testing.T) {
	client := llm.NewReplayClient(llm.TextResponse("要約です"))
	a := New(Config{
		Client: client, Model: "claude-opus-5-5",
		SystemPrompt: "あなたはエージェントです",
		Tools:        []tools.Tool{&fakeTool{name: "read_file"}},
	})
	a.history = []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("a.goを読んで")),
	}
	if err := a.Compact(t.Context()); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	req := client.Requests()[0]
	if len(req.Tools) != 1 {
		t.Errorf("要約リクエストにtoolsが引き継がれていない: %d件", len(req.Tools))
	}
	if len(req.System) != 1 {
		t.Errorf("要約リクエストにsystemが引き継がれていない: %d件", len(req.System))
	}
	// tools を渡す以上、ツール呼び出しで応えられては困る。
	if req.ToolChoice.OfNone == nil {
		t.Errorf("tool_choice が none になっていない: %+v", req.ToolChoice)
	}
	if req.MaxTokens != summaryMaxTokens {
		t.Errorf("要約用のmax_tokensが使われていない: %d", req.MaxTokens)
	}
}

// 要約が空のときは、会話履歴を置き換えずにエラーを返すこと。
// 空の要約で置き換えると、ここまでの会話が黙って失われる。
func TestCompactKeepsHistoryWhenSummaryIsEmpty(t *testing.T) {
	client := llm.NewReplayClient(llm.TextResponse(""))
	a := New(Config{Client: client, Model: "claude-opus-5-5"})
	a.history = []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("REPLを実装して")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("実装しました")),
	}
	if err := a.Compact(t.Context()); err == nil {
		t.Fatal("空の要約はエラーになるべき")
	}
	if n := len(a.History()); n != 2 {
		t.Errorf("会話履歴が置き換えられた: %d件", n)
	}
}
