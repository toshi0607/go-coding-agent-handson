package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/final/llm"
	"github.com/toshi0607/go-coding-agent-handson/final/permission"
	"github.com/toshi0607/go-coding-agent-handson/final/tools"
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

// allowAll は確認なしで全ツールを許可する Checker(テスト用)。
func allowAll(names ...string) *permission.Checker {
	policies := map[string]permission.Policy{}
	for _, n := range names {
		policies[n] = permission.Allow
	}
	return permission.New(permission.Config{ToolPolicies: policies})
}

func TestTextOnlyTurn(t *testing.T) {
	client := llm.NewReplayClient(llm.TextResponse("こんにちは!"))
	a := New(Config{Client: client, Model: "claude-opus-5-5"})

	got, err := a.Run(t.Context(), "やあ")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "こんにちは!" {
		t.Errorf("got %q", got)
	}
	// 履歴 = ユーザー入力 + アシスタントのレスポンス の2件。
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
		Client: client, Model: "claude-opus-5-5",
		Tools:      []tools.Tool{tool},
		Permission: allowAll("get_time"),
	})

	got, err := a.Run(t.Context(), "いま何時？")
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

func TestDeniedToolReturnsErrorResult(t *testing.T) {
	tool := &fakeTool{name: "edit_file", result: "編集しました"}
	client := llm.NewReplayClient(
		llm.ToolUseResponse("toolu_1", "edit_file", `{"path":"a.go"}`),
		llm.TextResponse("わかりました、編集は中止します"),
	)
	// Askerなし → 承認が必要なツールは拒否される。
	a := New(Config{
		Client: client, Model: "claude-opus-5-5",
		Tools:      []tools.Tool{tool},
		Permission: permission.New(permission.Config{}),
	})

	got, err := a.Run(t.Context(), "a.goを編集して")
	if err != nil {
		t.Fatalf("拒否はターンを止めない(LLMに返して継続する)はず: %v", err)
	}
	if tool.calls != 0 {
		t.Error("拒否されたツールが実行された")
	}
	if got != "わかりました、編集は中止します" {
		t.Errorf("got %q", got)
	}
	// 拒否は is_error 付きの tool_result としてLLMに伝わる。
	second, _ := json.Marshal(client.Requests()[1].Messages)
	if !strings.Contains(string(second), "is_error") {
		t.Errorf("拒否がis_errorとして伝わっていない: %s", second)
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	client := llm.NewReplayClient(
		llm.ToolUseResponse("toolu_1", "no_such_tool", `{}`),
		llm.TextResponse("失礼しました"),
	)
	a := New(Config{Client: client, Model: "claude-opus-5-5"})

	if _, err := a.Run(t.Context(), "何かして"); err != nil {
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
		Client: client, Model: "claude-opus-5-5",
		Tools:         []tools.Tool{tool},
		Permission:    allowAll("spin"),
		MaxIterations: 2,
	})

	got, err := a.Run(t.Context(), "無限に働いて")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(got, "上限の") {
		t.Errorf("上限到達の報告がない: %q", got)
	}
	if tool.calls != 2 {
		t.Errorf("上限2回で止まるはず: %d回", tool.calls)
	}
}

// max_tokens でレスポンスが切れると、生成途中の tool_use が履歴に残ることがある。
// 対応する tool_result を足さないまま次のターンに進むと、会話履歴を消すまで
// 以降すべてのリクエストがAPIに拒否される。その回帰テスト。
func TestInterruptedToolUseIsClosedOut(t *testing.T) {
	// stop_reason: max_tokens だが tool_use ブロックを含むレスポンス。
	interrupted := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
		"content":[{"type":"text","text":"編集します"},
		           {"type":"tool_use","id":"toolu_cut","name":"edit_file","input":{}}],
		"stop_reason":"max_tokens","usage":{"input_tokens":10,"output_tokens":5}}`
	client := llm.NewReplayClient(interrupted, llm.TextResponse("2ターン目の応答"))
	a := New(Config{Client: client, Model: "claude-opus-5-5", Permission: allowAll()})

	if _, err := a.Run(t.Context(), "a.goを直して"); err != nil {
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
	if _, err := a.Run(t.Context(), "続けて"); err != nil {
		t.Fatalf("次のターンが失敗した: %v", err)
	}
}

// stop_reason が tool_use なのに tool_use ブロックがない矛盾したレスポンス。
// 空のuserメッセージを送らず、上限まで無駄な往復もしないこと。
func TestToolUseStopReasonWithoutToolBlocks(t *testing.T) {
	inconsistent := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
		"content":[{"type":"text","text":"考え中"}],
		"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`
	client := llm.NewReplayClient(inconsistent)
	a := New(Config{Client: client, Model: "claude-opus-5-5"})

	got, err := a.Run(t.Context(), "何かして")
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

func TestTaskToolRunsSubAgent(t *testing.T) {
	// レスポンスは親→サブ→親の順に消費される:
	//   1. 親: taskツールを呼ぶ
	//   2. サブ: 調査結果を返す(サブエージェントのターン)
	//   3. 親: tool_result(=サブの結論)を受けて最終的なレスポンス
	client := llm.NewReplayClient(
		llm.ToolUseResponse("toolu_1", "task", `{"description":"調査","prompt":"認証処理の場所を調べて"}`),
		llm.TextResponse("認証処理は auth/login.go にあります"),
		llm.TextResponse("調査完了: auth/login.go を確認してください"),
	)

	taskTool := NewTaskTool(func() *Agent {
		return New(Config{Client: client, Model: "claude-opus-5-5"})
	})
	parent := New(Config{
		Client: client, Model: "claude-opus-5-5",
		Tools:      []tools.Tool{taskTool},
		Permission: allowAll("task"),
	})

	got, err := parent.Run(t.Context(), "認証処理どこ？")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "調査完了: auth/login.go を確認してください" {
		t.Errorf("got %q", got)
	}

	// 親の履歴は4件(user, assistant, tool_result, assistant)。
	// サブエージェントが何をしたかは親の履歴に残らない。
	if len(parent.History()) != 4 {
		t.Errorf("親の履歴は4件のはず: %d件", len(parent.History()))
	}
	// サブの結論だけが tool_result として親に渡る。
	third, _ := json.Marshal(client.Requests()[2].Messages)
	if !strings.Contains(string(third), "auth/login.go にあります") {
		t.Errorf("サブエージェントの結論が親に渡っていない: %s", third)
	}
}

// compactionを「tool_use と tool_result の対応が閉じている地点」だけで
// 行うことの不変条件テスト。
//
// ツールを呼んだレスポンスを履歴に足した直後(tool_result を足す前)に
// 履歴を要約すると、要約リクエストに tool_result のない tool_use が残り、
// APIに拒否される。閾値を極端に下げて、圧縮が tool_result を足した後と
// ターンの開始時に走り、どの要約リクエストでも対応が閉じていることを確認する。
func TestCompactionHappensOnlyWherePairsAreClosed(t *testing.T) {
	tool := &fakeTool{name: "spin", result: "ツールの結果"}
	client := llm.NewReplayClient(
		llm.ToolUseResponse("toolu_1", "spin", `{}`), // 1回目: ツール呼び出し
		llm.TextResponse("ここまでの要約"),                  // 2回目: tool_result を足した後の圧縮
		llm.TextResponse("終わりました"),                   // 3回目: ターン終了
		llm.TextResponse("ここまでの要約2"),                 // 4回目: 2ターン目の頭で走る圧縮
		llm.TextResponse("2ターン目の応答"),
	)
	a := New(Config{
		Client: client, Model: "claude-opus-5-5",
		Tools:      []tools.Tool{tool},
		Permission: allowAll("spin"),
		// 閾値1 = 1回でもレスポンスを受け取れば「圧縮すべき」状態になる。
		CompactThreshold: 1, MaxToolResultChars: 1000,
	})

	if _, err := a.Run(t.Context(), "やって"); err != nil {
		t.Fatal(err)
	}

	// 1ターン目のLLM呼び出しは、ツール呼び出し・要約・ターン終了の3回。
	// 2回なら tool_result を足した後の圧縮が走っていない。
	reqs := client.Requests()
	if len(reqs) != 3 {
		t.Fatalf("tool_result を足した後の圧縮が走っていない: LLM呼び出し%d回", len(reqs))
	}
	// 要約リクエストの履歴で、tool_use に tool_result が対応していること。
	// 件数も確かめないと、tool_use が無いせいで「何も見つからないので合格」に
	// なっていても気づけない。
	if n := assertToolUsesAreClosed(t, reqs[1].Messages); n != 1 {
		t.Fatalf("要約リクエストに tool_use が1件あるはず: %d件", n)
	}

	// ターンが終われば、次のターンの開始時にも圧縮は走る(閾値を超えているので)。
	if _, err := a.Run(t.Context(), "続けて"); err != nil {
		t.Fatal(err)
	}
	if n := len(client.Requests()); n != 5 {
		t.Errorf("ターンの開始時の圧縮が走っていない: LLM呼び出し%d回", n)
	}
	for _, req := range client.Requests() {
		assertToolUsesAreClosed(t, req.Messages)
	}
	assertToolUsesAreClosed(t, a.History())
}

// assertToolUsesAreClosed は履歴中のすべての tool_use に、
// 直後のメッセージで対応する tool_result があることを確認し、
// 検査した tool_use の件数を返す。
// これが崩れた履歴はAPIに拒否される。
func assertToolUsesAreClosed(t *testing.T, history []anthropic.MessageParam) int {
	t.Helper()
	found := 0
	for i, msg := range history {
		var pending []string
		for _, block := range msg.Content {
			if block.OfToolUse != nil {
				pending = append(pending, block.OfToolUse.ID)
			}
		}
		found += len(pending)
		if len(pending) == 0 {
			continue
		}
		if i+1 >= len(history) {
			t.Errorf("履歴%d件目の tool_use %v に対応する tool_result がない", i, pending)
			continue
		}
		answered := map[string]bool{}
		for _, block := range history[i+1].Content {
			if block.OfToolResult != nil {
				answered[block.OfToolResult.ToolUseID] = true
			}
		}
		for _, id := range pending {
			if !answered[id] {
				t.Errorf("tool_use %s に対応する tool_result がない", id)
			}
		}
	}
	return found
}

func TestAutoCompaction(t *testing.T) {
	client := llm.NewReplayClient(
		llm.TextResponse("1回目の応答"),          // usage 150トークン > 閾値100
		llm.TextResponse("これまでの要約: REPLの話"), // compaction用の要約
		llm.TextResponse("2回目の応答"),
	)
	a := New(Config{
		Client: client, Model: "claude-opus-5-5",
		CompactThreshold: 100, // ReplayClientのusageは毎回150(入力100+出力50)
	})

	if _, err := a.Run(t.Context(), "1回目"); err != nil {
		t.Fatal(err)
	}
	// 2ターン目の開始時に自動compactionが走る。
	if _, err := a.Run(t.Context(), "2回目"); err != nil {
		t.Fatal(err)
	}

	// 履歴 = 要約 + 2回目のuser + 2回目のassistant の3件。
	h := a.History()
	if len(h) != 3 {
		t.Fatalf("圧縮後の履歴は3件のはず: %d件", len(h))
	}
	first, _ := json.Marshal(h[0])
	if !strings.Contains(string(first), "要約") {
		t.Errorf("履歴の先頭が要約になっていない: %s", first)
	}
}
