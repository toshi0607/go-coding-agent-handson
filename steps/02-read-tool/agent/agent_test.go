package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/toshi0607/go-coding-agent-handson/steps/02-read-tool/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/02-read-tool/tools"
)

// fakeTool はテスト用の固定結果ツール。
type fakeTool struct {
	name   string
	result string
	calls  int
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "テスト用" }
func (f *fakeTool) InputSchema() tools.Schema {
	return tools.Schema{Properties: map[string]any{"arg": tools.StringProperty("引数")}}
}
func (f *fakeTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	f.calls++
	return f.result, nil
}

func TestTextOnlyTurn(t *testing.T) {
	client := llm.NewReplayClient(llm.TextResponse("こんにちは!"))
	a := New(Config{Client: client, Model: "claude-opus-5"})

	got, err := a.Run(context.Background(), "やあ")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "こんにちは!" {
		t.Errorf("got %q", got)
	}
	// 履歴 = ユーザー入力 + アシスタント応答 の2件。
	if len(a.History()) != 2 {
		t.Errorf("履歴は2件のはず: %d件", len(a.History()))
	}
}

func TestToolLoop(t *testing.T) {
	tool := &fakeTool{name: "get_time", result: "12:34"}
	client := llm.NewReplayClient(
		llm.ToolUseResponse("toolu_1", "get_time", `{}`),
		llm.TextResponse("現在時刻は12:34です"),
	)
	a := New(Config{
		Client: client, Model: "claude-opus-5",
		Tools: []tools.Tool{tool},
	})

	got, err := a.Run(context.Background(), "いま何時?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "現在時刻は12:34です" {
		t.Errorf("got %q", got)
	}
	if tool.calls != 1 {
		t.Errorf("ツールは1回実行されるはず: %d回", tool.calls)
	}

	// 2回目のリクエストには tool_result が含まれ、ツールの結果が渡っている。
	reqs := client.Requests()
	if len(reqs) != 2 {
		t.Fatalf("LLM呼び出しは2回のはず: %d回", len(reqs))
	}
	second, _ := json.Marshal(reqs[1].Messages)
	if !strings.Contains(string(second), "tool_result") || !strings.Contains(string(second), "12:34") {
		t.Errorf("2回目のリクエストにツール結果がない: %s", second)
	}

	// 履歴 = user, assistant(tool_use), user(tool_result), assistant の4件。
	if len(a.History()) != 4 {
		t.Errorf("履歴は4件のはず: %d件", len(a.History()))
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	client := llm.NewReplayClient(
		llm.ToolUseResponse("toolu_1", "no_such_tool", `{}`),
		llm.TextResponse("失礼しました"),
	)
	a := New(Config{Client: client, Model: "claude-opus-5"})

	if _, err := a.Run(context.Background(), "何かして"); err != nil {
		t.Fatalf("未知ツールもエラー結果として返して継続するはず: %v", err)
	}
	second, _ := json.Marshal(client.Requests()[1].Messages)
	if !strings.Contains(string(second), "存在しません") {
		t.Errorf("未知ツールのエラーが伝わっていない: %s", second)
	}
}

func TestMaxIterationsStopsRunawayLoop(t *testing.T) {
	tool := &fakeTool{name: "spin", result: "まだ終わらない"}
	// LLMが延々とツールを呼び続けるシナリオ。
	client := llm.NewReplayClient(
		llm.ToolUseResponse("toolu_1", "spin", `{}`),
		llm.ToolUseResponse("toolu_2", "spin", `{}`),
		llm.ToolUseResponse("toolu_3", "spin", `{}`),
	)
	a := New(Config{
		Client: client, Model: "claude-opus-5",
		Tools:         []tools.Tool{tool},
		MaxIterations: 2,
	})

	got, err := a.Run(context.Background(), "無限に働いて")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(got, "打ち切り") {
		t.Errorf("上限到達の告知がない: %q", got)
	}
	if tool.calls != 2 {
		t.Errorf("上限2回で止まるはず: %d回", tool.calls)
	}
}

// max_tokens で応答が切れると、生成途中の tool_use が履歴に残ることがある。
// 対応する tool_result を積まないまま次のターンに進むと、以降すべての
// リクエストがAPIに拒否されて会話が死ぬ。その回帰テスト。
func TestInterruptedToolUseIsClosedOut(t *testing.T) {
	// stop_reason: max_tokens だが tool_use ブロックを含む応答。
	interrupted := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",
		"content":[{"type":"text","text":"編集します"},
		           {"type":"tool_use","id":"toolu_cut","name":"edit_file","input":{}}],
		"stop_reason":"max_tokens","usage":{"input_tokens":10,"output_tokens":5}}`
	client := llm.NewReplayClient(interrupted, llm.TextResponse("2ターン目の応答"))
	a := New(Config{Client: client, Model: "claude-opus-5"})

	if _, err := a.Run(context.Background(), "a.goを直して"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 履歴 = user, assistant(tool_use), user(tool_result) の3件。
	// tool_use が閉じられていること。
	h := a.History()
	if len(h) != 3 {
		t.Fatalf("中断されたtool_useが閉じられていない: 履歴%d件", len(h))
	}
	last, _ := json.Marshal(h[2])
	if !strings.Contains(string(last), "toolu_cut") || !strings.Contains(string(last), "tool_result") {
		t.Errorf("tool_useに対応するtool_resultがない: %s", last)
	}

	// 次のターンも正常に続けられる。
	if _, err := a.Run(context.Background(), "続けて"); err != nil {
		t.Fatalf("次のターンが失敗した: %v", err)
	}
}

// stop_reason が tool_use なのに tool_use ブロックがない矛盾応答。
// 空のuserメッセージを送らず、上限まで無駄な往復もしないこと。
func TestToolUseStopReasonWithoutToolBlocks(t *testing.T) {
	inconsistent := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",
		"content":[{"type":"text","text":"考え中"}],
		"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`
	client := llm.NewReplayClient(inconsistent)
	a := New(Config{Client: client, Model: "claude-opus-5"})

	got, err := a.Run(context.Background(), "何かして")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "考え中" {
		t.Errorf("got %q", got)
	}
	// LLM呼び出しは1回で止まる(空メッセージを送って往復を続けない)。
	if n := len(client.Requests()); n != 1 {
		t.Errorf("LLM呼び出しは1回のはず: %d回", n)
	}
}
