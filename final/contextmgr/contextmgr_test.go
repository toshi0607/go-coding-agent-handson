package contextmgr

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/final/llm"
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
	compacted, err := m.Compact(context.Background(), client, "claude-opus-5", history)
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
}
