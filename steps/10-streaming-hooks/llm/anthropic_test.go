package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ストリーミングは実APIを叩かずに検証する。
// httptest でSSE(Server-Sent Events)を模したレスポンスを返し、
// Accumulate が断片を1つの Message に組み立て直せているかを見る。
//
// ここを未検証のまま「動いているはず」にしておくと、SDKを上げたときに
// 壊れても誰も気づかない。エージェントの出力経路そのものなので、
// 偽のサーバーを立ててでも押さえておく価値がある。
func newSSEClient(t *testing.T, events []string) *AnthropicClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprint(w, e)
		}
	}))
	t.Cleanup(server.Close)

	return NewAnthropicClient(
		option.WithBaseURL(server.URL),
		option.WithAPIKey("test-key"),
		option.WithMaxRetries(0),
	)
}

// sse は1つのSSEイベントを組み立てる。実際のワイヤーフォーマットは
//
//	event: <名前>\n
//	data: <JSON>\n
//	\n
//
// という3行1組の繰り返しである。
func sse(name, data string) string {
	return "event: " + name + "\ndata: " + data + "\n\n"
}

func TestStreamAccumulatesTextAndToolUse(t *testing.T) {
	client := newSSEClient(t, []string{
		sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`),
		// 1つ目のブロック: テキストが3分割で届く。
		sse("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ファイルを"}}`),
		sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"読み"}}`),
		sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ます"}}`),
		sse("content_block_stop", `{"type":"content_block_stop","index":0}`),
		// 2つ目のブロック: tool_use の入力JSONも分割で届く。
		sse("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}`),
		sse("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`),
		sse("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"main.go\"}"}}`),
		sse("content_block_stop", `{"type":"content_block_stop","index":1}`),
		sse("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":25}}`),
		sse("message_stop", `{"type":"message_stop"}`),
	})

	var streamed strings.Builder
	resp, err := client.Stream(context.Background(), anthropic.MessageNewParams{
		Model:     "claude-opus-5",
		MaxTokens: 100,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("main.goを読んで"))},
	}, func(s string) { streamed.WriteString(s) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// onText には text_delta だけが流れる。
	// tool_use の入力JSONの断片が混ざっていたら、画面に生のJSONが漏れる。
	if got := streamed.String(); got != "ファイルを読みます" {
		t.Errorf("onTextに流れたテキスト: %q", got)
	}

	// 戻り値は断片ではなく組み立て直された完全なメッセージ。
	// 履歴に積むのはこちらである。
	if len(resp.Content) != 2 {
		t.Fatalf("ブロックは2つのはず: %+v", resp.Content)
	}
	if resp.Content[0].Text != "ファイルを読みます" {
		t.Errorf("テキストブロックが組み立てられていない: %q", resp.Content[0].Text)
	}
	toolUse := resp.Content[1]
	if toolUse.Type != "tool_use" || toolUse.Name != "read_file" || toolUse.ID != "toolu_1" {
		t.Errorf("tool_useブロックが組み立てられていない: %+v", toolUse)
	}
	if got := string(toolUse.Input); got != `{"path":"main.go"}` {
		t.Errorf("分割されたinput_json_deltaが繋がっていない: %q", got)
	}

	// message_delta で後から届く stop_reason と usage も反映される。
	if resp.StopReason != anthropic.StopReasonToolUse {
		t.Errorf("stop_reason: got %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 25 {
		t.Errorf("usageが反映されていない: %+v", resp.Usage)
	}

	// ToParam() で履歴に積める形になること(ここがSDKの落とし穴で、
	// 内部のJSONメタデータが揃っていないと空になる)。
	data, err := resp.ToParam().MarshalJSON()
	if err != nil {
		t.Fatalf("ToParam: %v", err)
	}
	if !strings.Contains(string(data), "read_file") || !strings.Contains(string(data), "ファイルを読みます") {
		t.Errorf("履歴に積む形に変換できていない: %s", data)
	}
}

// onText が nil でも落ちないこと(サブエージェントは表示しない)。
func TestStreamWithoutCallback(t *testing.T) {
	client := newSSEClient(t, []string{
		sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`),
		sse("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"了解"}}`),
		sse("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sse("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`),
		sse("message_stop", `{"type":"message_stop"}`),
	})

	resp, err := client.Stream(context.Background(), anthropic.MessageNewParams{
		Model: "claude-opus-5", MaxTokens: 100,
	}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if resp.Content[0].Text != "了解" {
		t.Errorf("got %+v", resp.Content)
	}
}

// ストリームの途中で切れた場合にエラーを返すこと。
// 途中まで組み立てたメッセージを「完成品」として返してしまうと、
// 壊れた履歴が積まれて以降のリクエストが通らなくなる。
func TestStreamReportsServerError(t *testing.T) {
	client := newSSEClient(t, []string{
		sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`),
		sse("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`),
	})

	if _, err := client.Stream(context.Background(), anthropic.MessageNewParams{
		Model: "claude-opus-5", MaxTokens: 100,
	}, nil); err == nil {
		t.Fatal("ストリーム中のエラーが握りつぶされている")
	}
}
