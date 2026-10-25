package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toshi0607/go-coding-agent-handson/final/tools"
)

// Tool はMCPサーバーのツールを tools.Tool として使えるようにする
// アダプタ。エージェントループから見れば、組み込みツールもMCPツールも
// 同じ Tool インターフェースであり、区別する必要がない。
// step 02 で Tool インターフェースを決めておいたので、外部のツールも
// エージェント本体を変えずに渡せる。
type Tool struct {
	client      *Client
	name        string // サーバー上のツール名(接頭辞なし)
	description string
	schema      tools.Schema
}

// Tools はサーバーの全ツールを tools.Tool のリストとして返す。
func (c *Client) Tools(ctx context.Context) ([]tools.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	// session.Tools は tools/list を呼び、一覧がページに分かれていれば
	// 続きも取りながら1件ずつ返すイテレータ。
	var result []tools.Tool
	for info, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("MCPサーバー %s のツール一覧を取得できませんでした: %w", c.serverName, err)
		}
		schema, err := toSchema(info.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("MCPツール %s のスキーマを読み取れませんでした: %w", info.Name, err)
		}
		result = append(result, &Tool{
			client:      c,
			name:        info.Name,
			description: info.Description,
			schema:      schema,
		})
	}
	return result, nil
}

// toSchema はMCPの inputSchema(JSON Schema)から properties と required を
// 取り出して tools.Schema に写す。
//
// クライアントが受け取る inputSchema は、JSONをデコードしただけの値
// (map[string]any)である。いったんJSONに戻し、必要なフィールドだけを読む。
func toSchema(inputSchema any) (tools.Schema, error) {
	data, err := json.Marshal(inputSchema)
	if err != nil {
		return tools.Schema{}, err
	}
	var s struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return tools.Schema{}, err
	}
	return tools.Schema{Properties: s.Properties, Required: s.Required}, nil
}

// Name はツール名を "mcp__サーバー名__ツール名" 形式で返す。
// 複数のサーバーが同名のツールを提供しても衝突しないようにするための
// 名前空間である。
func (t *Tool) Name() string {
	return fmt.Sprintf("mcp__%s__%s", t.client.serverName, t.name)
}

func (t *Tool) Description() string {
	return t.description
}

func (t *Tool) InputSchema() tools.Schema {
	return t.schema
}

// Run は tools/call でサーバーのツールを実行し、テキストの結果を返す。
func (t *Tool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	// 応答待ちに上限を置く。ツール実行の ctx を親にするので、
	// Ctrl-C によるキャンセルもサーバーへの問い合わせまで届く。
	// 期限が来るかキャンセルされると、SDK はサーバーに
	// notifications/cancelled を送り、待つのをやめて ctx.Err() を返す。
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	res, err := t.client.session.CallTool(ctx, &sdk.CallToolParams{
		Name:      t.name,
		Arguments: input,
	})
	if err != nil {
		return "", fmt.Errorf("MCPサーバー %s のツール %s を呼び出せませんでした: %w", t.client.serverName, t.name, err)
	}
	// ツールの失敗は、JSON-RPCのエラーではなく isError 付きの結果で
	// 返ってくる。error にして返せば、エージェントループが is_error 付きの
	// tool_result としてLLMに渡す(組み込みツールの失敗と同じ扱い)。
	text := textContent(res.Content)
	if res.IsError {
		return "", fmt.Errorf("MCPツール %s がエラーを返しました: %s", t.name, text)
	}
	return text, nil
}

// textContent は結果の content からテキストだけをつなげて返す。
// 画像などテキスト以外の content は、このエージェントでは扱わない。
func textContent(content []sdk.Content) string {
	var b strings.Builder
	for _, c := range content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
