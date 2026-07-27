package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestReplayTextResponse(t *testing.T) {
	c := NewReplayClient(TextResponse("こんにちは"))

	resp, err := c.Complete(context.Background(), anthropic.MessageNewParams{})
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

// ToParam(応答→履歴の変換)がリプレイでも本物同様に動くことの確認。
// SDKのMessage型は内部にJSONメタデータを持つため、これが動くことは
// 「ワイヤーフォーマットからUnmarshalする」実装方針の正しさの証明になる。
func TestReplayMessageRoundTrip(t *testing.T) {
	c := NewReplayClient(TextResponse("応答テキスト"))
	resp, err := c.Complete(context.Background(), anthropic.MessageNewParams{})
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

func TestReplayExhausted(t *testing.T) {
	c := NewReplayClient(TextResponse("1つだけ"))
	if _, err := c.Complete(context.Background(), anthropic.MessageNewParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(context.Background(), anthropic.MessageNewParams{}); err == nil {
		t.Fatal("レスポンスを使い切ったらエラーになるべき")
	}
}
