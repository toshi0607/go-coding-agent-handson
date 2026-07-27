package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/toshi0607/go-coding-agent-handson/steps/01-repl/llm"
)

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

// 会話が「続く」ことの検証。
//
// Messages APIはステートレスなので、2ターン目のリクエストに
// 1ターン目のユーザー入力とアシスタント応答を含めるのは
// エージェント側の責務である。どちらかを積み忘れると、LLMは
// 会話の途中経過を知らないまま応答することになる。
func TestHistoryCarriesAcrossTurns(t *testing.T) {
	client := llm.NewReplayClient(
		llm.TextResponse("こんにちは、Gopherさん!"),
		llm.TextResponse("あなたの名前はGopherさんです"),
	)
	a := New(Config{Client: client, Model: "claude-opus-5"})

	if _, err := a.Run(context.Background(), "私はGopherです"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "私の名前を覚えていますか?"); err != nil {
		t.Fatal(err)
	}

	reqs := client.Requests()
	if len(reqs) != 2 {
		t.Fatalf("LLM呼び出しは2回のはず: %d回", len(reqs))
	}
	// 2回目のリクエストは [user, assistant, user] の3件を含む。
	if got := len(reqs[1].Messages); got != 3 {
		t.Fatalf("2回目のリクエストは履歴3件を含むはず: %d件", got)
	}
	second, _ := json.Marshal(reqs[1].Messages)
	for _, want := range []string{"私はGopherです", "こんにちは、Gopherさん!", "私の名前を覚えていますか?"} {
		if !strings.Contains(string(second), want) {
			t.Errorf("2回目のリクエストに %q が含まれていない: %s", want, second)
		}
	}

	// 手元の履歴は4件(user, assistant, user, assistant)。
	if len(a.History()) != 4 {
		t.Errorf("履歴は4件のはず: %d件", len(a.History()))
	}
}
