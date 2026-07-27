package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/toshi0607/go-coding-agent-handson/steps/07-subagent/tools"
)

// TaskTool はサブエージェントを起動するツール。
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
type TaskTool struct {
	// newSubAgent はサブエージェントを生成するファクトリ。
	// どんなツール・権限を持たせるかは呼び出し側(main)が決める。
	newSubAgent func() *Agent
}

// NewTaskTool は TaskTool を作る。
// newSubAgent が返すエージェントには TaskTool 自身を含めないこと。
// 含めるとサブエージェントがさらにサブエージェントを起動でき、
// 制御不能な再帰(fork爆弾のようなもの)につながる。
func NewTaskTool(newSubAgent func() *Agent) *TaskTool {
	return &TaskTool{newSubAgent: newSubAgent}
}

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Description() string {
	return "サブエージェントにタスクを委任します。複数ファイルにまたがる調査や検索など、" +
		"途中経過よりも最終的な結論だけが欲しいタスクに使ってください。" +
		"サブエージェントはこれまでの会話を一切知らないため、prompt には" +
		"背景・対象・期待する出力形式など、単独で作業できるだけの情報をすべて含めてください。"
}

func (t *TaskTool) InputSchema() tools.Schema {
	return tools.Schema{
		Properties: map[string]any{
			"description": tools.StringProperty("タスクの短い説明(進捗表示用)。例: 認証処理の調査"),
			"prompt":      tools.StringProperty("サブエージェントへの指示。自己完結していること。"),
		},
		Required: []string{"description", "prompt"},
	}
}

func (t *TaskTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("入力のパースに失敗: %w", err)
	}
	if in.Prompt == "" {
		return "", fmt.Errorf("prompt を指定してください")
	}

	// サブエージェントは毎回まっさらな状態で作る。
	// 会話履歴は空。ここが「コンテキスト分離」の実装のすべてである。
	sub := t.newSubAgent()
	result, err := sub.Run(ctx, in.Prompt)
	if err != nil {
		return "", fmt.Errorf("サブエージェントの実行に失敗: %w", err)
	}
	// 親に返るのはこの最終テキストだけ。サブエージェントが何十回
	// ツールを呼んでいても、親の履歴にはこの文字列しか残らない。
	return result, nil
}
