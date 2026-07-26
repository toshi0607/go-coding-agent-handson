package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/toshi0607/go-coding-agent-handson/final/tools"
)

// Tool はMCPサーバーのツールを tools.Tool として使えるようにする
// アダプタ。エージェント本体から見れば、組み込みツールもMCPツールも
// 同じ Tool インターフェースであり、区別する必要がない。
// 「ツールという抽象を最初に切っておいたおかげで、外部ツールが
// そのまま差し込める」という、インターフェース設計の効能の実例である。
type Tool struct {
	client *Client
	info   ToolInfo
	schema tools.Schema
}

// Tools はサーバーの全ツールを tools.Tool のリストとして返す。
func (c *Client) Tools() ([]tools.Tool, error) {
	infos, err := c.ListTools()
	if err != nil {
		return nil, err
	}
	var result []tools.Tool
	for _, info := range infos {
		// MCPのinputSchemaはJSON Schemaそのものなので、
		// properties と required を取り出して tools.Schema に写す。
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if len(info.InputSchema) > 0 {
			if err := json.Unmarshal(info.InputSchema, &schema); err != nil {
				return nil, fmt.Errorf("MCPツール %s のスキーマをパースできません: %w", info.Name, err)
			}
		}
		result = append(result, &Tool{
			client: c,
			info:   info,
			schema: tools.Schema{Properties: schema.Properties, Required: schema.Required},
		})
	}
	return result, nil
}

// Name はツール名を "mcp__サーバー名__ツール名" 形式で返す。
// 複数のサーバーが同名のツールを提供しても衝突しないようにするための
// 名前空間である。
func (t *Tool) Name() string {
	return fmt.Sprintf("mcp__%s__%s", t.client.serverName, t.info.Name)
}

func (t *Tool) Description() string {
	return t.info.Description
}

func (t *Tool) InputSchema() tools.Schema {
	return t.schema
}

func (t *Tool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	return t.client.CallTool(t.info.Name, input)
}
