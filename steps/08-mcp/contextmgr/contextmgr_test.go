package contextmgr

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/08-mcp/llm"
)

func TestTruncateToolResult(t *testing.T) {
	m := &Manager{MaxToolResultChars: 10}

	if got := m.TruncateToolResult("short"); got != "short" {
		t.Errorf("短い結果は切り詰めない: %q", got)
	}

	long := strings.Repeat("a", 100)
	got := m.TruncateToolResult(long)
	if !strings.HasPrefix(got, strings.Repeat("a", 10)) {
		t.Errorf("先頭が保存されていない: %q", got)
	}
	// 切り詰めた事実がLLMに伝わることが重要。
	if !strings.Contains(got, "切り詰め") {
		t.Errorf("切り詰めの告知がない: %q", got)
	}
}

// バイト単位で切ると日本語がちょうど切れ目で壊れる。
// 「10文字で切り詰めた」と告知しながら壊れたバイト列を送るのは、
// LLMにとってもユーザーにとっても嘘になる。
func TestTruncateToolResultKeepsUTF8Intact(t *testing.T) {
	m := &Manager{MaxToolResultChars: 10}

	// 1文字3バイトの日本語30文字 = 90バイト。
	long := strings.Repeat("あ", 30)
	got := m.TruncateToolResult(long)

	if !strings.HasPrefix(got, strings.Repeat("あ", 10)) {
		t.Errorf("先頭10文字が保存されていない: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("UTF-8が壊れている: %q", got)
	}
	// 告知の数字も文字数であること(バイト数の90ではない)。
	if !strings.Contains(got, "全体は 30 文字") {
		t.Errorf("全体の文字数がバイト数になっている: %q", got)
	}

	// ちょうど上限ぴったりのときは切り詰めない。
	exact := strings.Repeat("あ", 10)
	if got := m.TruncateToolResult(exact); got != exact {
		t.Errorf("上限ぴったりは切り詰めないはず: %q", got)
	}
}

func TestShouldCompact(t *testing.T) {
	m := &Manager{CompactThreshold: 1000}
	if m.ShouldCompact(999) {
		t.Error("閾値未満でcompactionすべきでない")
	}
	if !m.ShouldCompact(1001) {
		t.Error("閾値超過でcompactionすべき")
	}
}

func TestCompact(t *testing.T) {
	client := llm.NewReplayClient(
		llm.TextResponse("ユーザーはREPLの実装を依頼。main.goに実装済み。次はテストを書く。"),
	)
	m := NewManager()

	history := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("REPLを実装して")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("実装しました")),
	}
	base := anthropic.MessageNewParams{
		Model:    "claude-opus-5",
		System:   []anthropic.TextBlockParam{{Text: "あなたはエージェントです"}},
		Messages: history,
	}
	compacted, err := m.Compact(context.Background(), client, base)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// 履歴は要約1メッセージに置き換わる。
	if len(compacted) != 1 {
		t.Fatalf("圧縮後の履歴は1件のはず: %d件", len(compacted))
	}

	// 要約リクエストには元の履歴全体+要約指示が含まれる。
	reqs := client.Requests()
	if len(reqs) != 1 {
		t.Fatalf("LLM呼び出しは1回のはず: %d回", len(reqs))
	}
	if got := len(reqs[0].Messages); got != 3 {
		t.Errorf("要約リクエストは履歴2件+指示1件の3件のはず: %d件", got)
	}

	// 呼び出し側の履歴スライスを壊していないこと。
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
	m := NewManager()

	base := anthropic.MessageNewParams{
		Model:  "claude-opus-5",
		System: []anthropic.TextBlockParam{{Text: "あなたはエージェントです"}},
		Tools: []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
			Name:        "read_file",
			InputSchema: anthropic.ToolInputSchemaParam{},
		}}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("a.goを読んで")),
		},
	}
	if _, err := m.Compact(context.Background(), client, base); err != nil {
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
