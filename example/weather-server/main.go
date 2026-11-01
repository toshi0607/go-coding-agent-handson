// weather-server は8章の試し方で使う、例のMCPサーバー。
//
// get_weather ツールを1つだけ持ち、引数の city を受け取って「晴れ (city)」を返す。
// 天気は調べず、いつも同じ結果を返す(MCPのやりとりを試すための例なので)。
// MCPの公式Go SDKで書き、stdio transport で動く。
//
// example ディレクトリの .mcp.json が `go run ./weather-server` で起動する。
// 中身は mcp パッケージのテスト(mcp_test.go の newTestServer)の get_weather と同じ。
package main

import (
	"context"
	"log"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type weatherInput struct {
	City string `json:"city" jsonschema:"都市名"`
}

func main() {
	s := sdk.NewServer(&sdk.Implementation{Name: "weather", Version: "v0.1.0"}, nil)
	sdk.AddTool(s, &sdk.Tool{Name: "get_weather", Description: "天気を返す"},
		func(ctx context.Context, req *sdk.CallToolRequest, in weatherInput) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "晴れ (" + in.City + ")"}}}, nil, nil
		})

	// 標準入力と標準出力で MCPクライアントとやりとりする。標準出力は
	// JSON-RPC のメッセージ専用なので、ログは標準エラー出力に出す(log のデフォルト)。
	if err := s.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
