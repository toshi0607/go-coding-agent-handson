package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// ReplayClient はあらかじめ用意したAPIレスポンス(JSON)を順番に再生する
// テスト用クライアント。APIキーなしでエージェントの全ロジックを検証できる。
//
// レスポンスはMessages APIのワイヤーフォーマット(実際にHTTPで返ってくる
// JSON)そのままで持つ。SDKの Message 型は内部にJSONメタデータを持っていて、
// 構造体リテラルで組み立てると ToParam() などが正しく動かないため、
// 「本物と同じJSONをUnmarshalする」ことで本物と同じ挙動を再現している。
// 実APIのレスポンスを保存しておけば、そのまま再生(録画リプレイ)できる。
type ReplayClient struct {
	responses []string
	index     int
	requests  []anthropic.MessageNewParams
}

// NewReplayClient は再生するレスポンスJSONの列からクライアントを作る。
func NewReplayClient(responses ...string) *ReplayClient {
	return &ReplayClient{responses: responses}
}

// Requests はこれまでに受け取ったリクエストを返す(テストの検証用)。
func (c *ReplayClient) Requests() []anthropic.MessageNewParams {
	return c.requests
}

func (c *ReplayClient) next(params anthropic.MessageNewParams) (*anthropic.Message, error) {
	c.requests = append(c.requests, params)
	if c.index >= len(c.responses) {
		return nil, fmt.Errorf("llm.ReplayClient: %d件のレスポンスを使い切りました(%d回目の呼び出し)", len(c.responses), c.index+1)
	}
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(c.responses[c.index]), &msg); err != nil {
		return nil, fmt.Errorf("llm.ReplayClient: レスポンス%dのパースに失敗: %w", c.index, err)
	}
	c.index++
	return &msg, nil
}

func (c *ReplayClient) Complete(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	return c.next(params)
}

// 以下はテストでレスポンスJSONを手軽に作るためのヘルパー。
// ワイヤーフォーマットの形を知る教材も兼ねている。

// TextResponse はテキストだけで完結する(stop_reason: end_turn)レスポンスを作る。
func TextResponse(text string) string {
	return buildResponse([]map[string]any{
		{"type": "text", "text": text},
	}, "end_turn")
}

// ToolUseResponse はツール呼び出しを1つ含む(stop_reason: tool_use)レスポンスを作る。
// inputJSON はツールへの引数(JSONオブジェクトの文字列)。
func ToolUseResponse(toolUseID, toolName, inputJSON string) string {
	return buildResponse([]map[string]any{
		{"type": "text", "text": toolName + " を実行します。"},
		{"type": "tool_use", "id": toolUseID, "name": toolName, "input": json.RawMessage(inputJSON)},
	}, "tool_use")
}

func buildResponse(content []map[string]any, stopReason string) string {
	resp := map[string]any{
		"id":          "msg_replay",
		"type":        "message",
		"role":        "assistant",
		"model":       "claude-opus-5",
		"content":     content,
		"stop_reason": stopReason,
		"usage":       map[string]any{"input_tokens": 100, "output_tokens": 50},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		panic(err) // 固定構造のMarshalは失敗しない
	}
	return string(data)
}
