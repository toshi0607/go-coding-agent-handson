package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/toshi0607/go-coding-agent-handson/steps/07-subagent/tools"
)

// SubAgentTool はサブエージェントを起動するツール。
//
// なぜサブエージェントが必要か。調査タスクは大量のツール結果
// (ファイル一覧、grep結果、読んだファイルの中身)を生むが、
// 親エージェントに必要なのは最終的な結論だけであることが多い。
// 調査の過程を別エージェントの履歴に隔離し、結論のテキストだけを
// 受け取れば、親のコンテキストは汚れない。これがコンテキスト分離であり、
// サブエージェントの最大の存在意義である。
//
// サブエージェントに何を渡すか(そして何を渡さないか)が設計の核心:
//   - 渡すもの: prompt(タスクの説明)だけ
//   - 渡さないもの: 親の会話履歴
//
// 親の文脈を知らないからこそコンテキストが軽くなる。裏返すと、
// promptには「サブエージェントが単独で作業を完遂できるだけの情報」を
// すべて書き込む必要がある。LLMへのツール説明でそれを要求している。
type SubAgentTool struct {
	// cfg はサブエージェントの設定。agent ツールが呼ばれるたびに
	// New(cfg) で会話履歴が空の Agent を作る。
	// どんなツール・権限を持たせるかは呼び出し側(main)が決める。
	cfg Config
}

// NewSubAgentTool は SubAgentTool を作る。
// cfg.Tools には SubAgentTool 自身を含めないこと。
// 含めると入れ子にできるが、この実装は入れ子の深さを数えないので含めない。
func NewSubAgentTool(cfg Config) *SubAgentTool {
	return &SubAgentTool{cfg: cfg}
}

func (t *SubAgentTool) Name() string { return "agent" }

func (t *SubAgentTool) Description() string {
	return "サブエージェントにタスクを委任します。複数ファイルにまたがる調査や検索など、" +
		"途中経過よりも最終的な結論だけが欲しいタスクに使ってください。" +
		"サブエージェントはこれまでの会話を一切知らないため、prompt には" +
		"背景・対象・期待する出力形式など、単独で作業できるだけの情報をすべて含めてください。"
}

func (t *SubAgentTool) InputSchema() tools.Schema {
	return tools.Schema{
		Properties: map[string]any{
			"description": tools.StringProperty("タスクの短い説明(進捗表示用)。例: 認証処理の調査"),
			"prompt":      tools.StringProperty("サブエージェントへの指示。自己完結していること。"),
		},
		Required: []string{"description", "prompt"},
	}
}

func (t *SubAgentTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("入力のパースに失敗しました: %w", err)
	}
	if in.Prompt == "" {
		return "", fmt.Errorf("prompt を指定してください")
	}

	// TODO(step07-1): サブエージェントを起動してタスクを委任する。
	//
	//  - New(t.cfg) でサブエージェントを作る。New が返すのは
	//    会話履歴が空の、毎回まっさらなエージェントである
	//  - 渡すのは in.Prompt だけ。親の会話履歴は渡さない(そもそも
	//    渡せる口がないことを確認してほしい)。ここが「コンテキスト分離」の
	//    実装のすべてである
	//  - サブエージェントの Run の戻り値(最終テキスト)を返す。親の履歴に
	//    残るのはこのテキストだけで、サブエージェントが何十回ツールを
	//    呼んでいても、その過程は親のコンテキストを消費しない
	//
	// 数行で書けるが、テストは「親の履歴がサブに漏れていないか」
	// 「サブの結論だけが親に渡るか」まで見ている。
	panic("TODO(step07-1): steps/07-subagent/agent/subagent_tool.go を実装してください(hints.md 参照)")
}
