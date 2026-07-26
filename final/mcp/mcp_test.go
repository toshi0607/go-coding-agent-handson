package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
)

// fakeServer はメモリ上で動く疑似MCPサーバー。
// 子プロセスの代わりに io.Pipe で接続し、プロトコルのやり取りを検証する。
func fakeServer(t *testing.T) *Client {
	t.Helper()
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	go func() {
		scanner := bufio.NewScanner(serverReader)
		for scanner.Scan() {
			var req struct {
				ID     *int            `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
				t.Errorf("サーバー: 不正なリクエスト: %v", err)
				return
			}
			if req.ID == nil {
				continue // notifications/initialized などの通知は応答不要
			}

			var result any
			switch req.Method {
			case "initialize":
				result = map[string]any{
					"protocolVersion": protocolVersion,
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "fake", "version": "0.0.1"},
				}
			case "tools/list":
				result = map[string]any{
					"tools": []map[string]any{{
						"name":        "get_weather",
						"description": "天気を返す",
						"inputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{"city": map[string]any{"type": "string"}},
							"required":   []string{"city"},
						},
					}},
				}
			case "tools/call":
				var p struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				_ = json.Unmarshal(req.Params, &p)
				result = map[string]any{
					"content": []map[string]any{{"type": "text", "text": "晴れ (" + p.Name + ")"}},
				}
			}

			resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
			if _, err := serverWriter.Write(append(resp, '\n')); err != nil {
				return
			}
		}
	}()

	c := newClient("weather", clientWriter, clientReader)
	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

func TestListAndCallTools(t *testing.T) {
	c := fakeServer(t)

	toolList, err := c.Tools()
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(toolList) != 1 {
		t.Fatalf("ツールは1つのはず: %d", len(toolList))
	}

	tool := toolList[0]
	// サーバー名で名前空間が切られる。
	if got := tool.Name(); got != "mcp__weather__get_weather" {
		t.Errorf("ツール名: got %q", got)
	}
	// JSON SchemaがSDK用のSchemaに写されている。
	schema := tool.InputSchema()
	if _, ok := schema.Properties["city"]; !ok {
		t.Errorf("スキーマにcityがない: %+v", schema)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "city" {
		t.Errorf("requiredが写されていない: %+v", schema.Required)
	}

	// ツール実行 = tools/call の往復。
	got, err := tool.Run(context.Background(), json.RawMessage(`{"city":"Tokyo"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "晴れ (get_weather)" {
		t.Errorf("got %q", got)
	}
}
