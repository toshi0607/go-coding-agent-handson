package llm

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestReplayTextResponse(t *testing.T) {
	c := NewReplayClient(TextResponse("こんにちは"))

	resp, err := c.Complete(t.Context(), anthropic.MessageNewParams{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.StopReason != anthropic.StopReasonEndTurn {
		t.Errorf("stop_reason: got %q", resp.StopReason)
	}
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "こんにちは" {
		t.Errorf("content: %+v", resp.Content)
	}
}

func TestReplayToolUseResponse(t *testing.T) {
	c := NewReplayClient(ToolUseResponse("toolu_1", "read_file", `{"path":"main.go"}`))

	resp, err := c.Complete(t.Context(), anthropic.MessageNewParams{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.StopReason != anthropic.StopReasonToolUse {
		t.Errorf("stop_reason: got %q", resp.StopReason)
	}
	var toolUse *anthropic.ContentBlockUnion
	for i := range resp.Content {
		if resp.Content[i].Type == "tool_use" {
			toolUse = &resp.Content[i]
		}
	}
	if toolUse == nil {
		t.Fatal("tool_useブロックがない")
	}
	if toolUse.Name != "read_file" || toolUse.ID != "toolu_1" {
		t.Errorf("tool_use: %+v", toolUse)
	}
	if got := string(toolUse.Input); got != `{"path":"main.go"}` {
		t.Errorf("input: %q", got)
	}
}

// ToParam(応答→履歴の変換)がリプレイでも本物同様に動くことの確認。
// SDKのMessage型は内部にJSONメタデータを持つため、これが動くことは
// 「ワイヤーフォーマットからUnmarshalする」実装方針の正しさの証明になる。
func TestReplayMessageRoundTrip(t *testing.T) {
	c := NewReplayClient(TextResponse("応答テキスト"))
	resp, err := c.Complete(t.Context(), anthropic.MessageNewParams{})
	if err != nil {
		t.Fatal(err)
	}

	param := resp.ToParam()
	data, err := param.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !strings.Contains(string(data), "応答テキスト") {
		t.Errorf("ToParamでテキストが失われた: %s", data)
	}
}

func TestReplayStreamCallsOnText(t *testing.T) {
	c := NewReplayClient(TextResponse("ストリーム"))
	var streamed strings.Builder
	resp, err := c.Stream(t.Context(), anthropic.MessageNewParams{}, func(s string) {
		streamed.WriteString(s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if streamed.String() != "ストリーム" {
		t.Errorf("onTextに流れたテキスト: %q", streamed.String())
	}
	if resp == nil || resp.Content[0].Text != "ストリーム" {
		t.Error("完全なメッセージも返るべき")
	}
}

func TestReplayExhausted(t *testing.T) {
	c := NewReplayClient(TextResponse("1つだけ"))
	if _, err := c.Complete(t.Context(), anthropic.MessageNewParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(t.Context(), anthropic.MessageNewParams{}); err == nil {
		t.Fatal("レスポンスを使い切ったらエラーになるべき")
	}
}
