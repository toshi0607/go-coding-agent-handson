// Package tools はエージェントが使うツールを定義する。
//
// ツールとは「名前 + 説明 + 入力スキーマ + 実行本体」の4点セットである。
// 前半3つはLLMに渡す「ツールの取扱説明書」で、LLMはこれだけを見て
// どのツールをどんな引数で呼ぶかを決める。実行本体(Run)を動かすのは
// LLMではなく、エージェント側(こちらのプログラム)の仕事である。
package tools

import (
	"context"
	"encoding/json"
)

// Tool はエージェントが実行できる1つのツールを表す。
type Tool interface {
	// Name はツール名。LLMはこの名前でツールを呼び出す。
	Name() string
	// Description はツールの説明。LLMがツール選択の判断に使うため、
	// 「いつ使うべきか」まで書くと精度が上がる。
	Description() string
	// InputSchema は入力のJSON Schema(type: object)。
	InputSchema() Schema
	// Run はツールを実行する。input はLLMが生成した引数(JSON)。
	// 失敗は error で返す。エージェントはこれを is_error 付きの
	// tool_result としてLLMに返し、LLMに立て直しの機会を与える。
	Run(ctx context.Context, input json.RawMessage) (string, error)
}

// Schema はツール入力のJSON Schemaを表す。
// トップレベルは常に {"type": "object"} なので、properties と required だけ持つ。
type Schema struct {
	// Properties は JSON Schema の properties(プロパティ名 → スキーマ)。
	Properties map[string]any
	// Required は必須プロパティ名のリスト。
	Required []string
}

// StringProperty は {"type": "string", "description": ...} を作るヘルパー。
func StringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
