package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toshi0607/go-coding-agent-handson/steps/08-mcp/tools"
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
	// TODO(step08-1): サーバーのツール一覧を取得し、1つずつ *Tool に
	// ラップして返す。満たすべき仕様:
	//
	//  - callTimeout の期限付き context を作って使う(応答しないサーバーで
	//    起動が止まらないように)
	//  - c.session.Tools(ctx, nil) で一覧を取る。これは tools/list を呼び、
	//    (*sdk.Tool, error) を1件ずつ返すイテレータなので、for range で回す。
	//    err が入っていたら、どのサーバーの一覧が取れなかったか分かる
	//    エラーを返す
	//  - 各ツールの InputSchema は toSchema(実装済み)で tools.Schema に写す
	//  - *Tool の name にはサーバー上の名前(info.Name)をそのまま入れる。
	//    "mcp__サーバー名__" の接頭辞は Name() が付ける
	panic("TODO(step08-1): steps/08-mcp/mcp/tool.go を実装してください(hints.md 参照)")
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
	// TODO(step08-2): サーバーのツールを呼び、テキストの結果を返す。
	// 満たすべき仕様:
	//
	//  - 引数の ctx を親に、callTimeout の期限付き context を作る。
	//    無期限に待つと、応答しないサーバー1つでエージェント全体が固まる。
	//    親が ctx なので、Ctrl-C によるキャンセルも届く
	//  - t.client.session.CallTool に、サーバー上の名前(t.name)と
	//    LLMが生成した引数(input)を渡す。
	//    失敗したら、どのサーバーのどのツールか分かるエラーを返す。
	//    期限切れを呼び出し側が errors.Is で判別できるよう %w で包む
	//  - 結果の content からテキストを取り出す(textContent は実装済み)
	//  - res.IsError が立っていたら、そのテキストを含むエラーを返す。
	//    ツールの失敗はJSON-RPCのエラーではなく、この形で返ってくる
	panic("TODO(step08-2): steps/08-mcp/mcp/tool.go を実装してください(hints.md 参照)")
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
