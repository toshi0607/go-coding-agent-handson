// Package agent はエージェントの本体、すなわち「ループ」を実装する。
//
// エージェントの正体は次のループである:
//
//	ユーザー入力 → LLM呼び出し → (ツール実行 → 結果を返してLLM再呼び出し)* → 応答
//
// LLMは「次にどのツールを使うか」を決めるだけで、実際に手を動かす
// (ファイルを読む、コマンドを実行する)のはこちら側のコードである。
// この役割分担と、その間を往復するメッセージの積み方さえ理解すれば、
// コーディングエージェントの骨格は理解できたことになる。
package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/contextmgr"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/permission"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/tools"
)

const (
	// maxTokensPerTurn は1回のLLM呼び出しの出力上限。
	maxTokensPerTurn = 8192

	// maxDisplayRunes はツール実行を画面に表示するときの引数の文字数上限。
	maxDisplayRunes = 120

	// defaultMaxIterations は1ユーザーターン内のLLM往復回数の上限。
	// LLMがツールを呼び続ける限りループは回るので、暴走(同じ失敗を
	// 延々と繰り返す等)への保険として上限を置く。上限に達したら
	// 打ち切ってユーザーに制御を返す。
	defaultMaxIterations = 25
)

// Config はエージェントの構成。
type Config struct {
	Client llm.Client
	Model  anthropic.Model
	// SystemPrompt はシステムプロンプト(prompt.Build で作る)。
	SystemPrompt string
	// Tools はこのエージェントが使えるツール。
	Tools []tools.Tool
	// Permission はツール実行の承認担当。
	Permission *permission.Checker
	// ContextManager はコンテキスト管理担当。
	ContextManager *contextmgr.Manager
	// Out は途中経過(ストリーミングテキスト、ツール実行表示)の出力先。
	Out io.Writer
	// MaxIterations は1ターン内のLLM往復上限(0ならデフォルト)。
	MaxIterations int
}

// Agent は会話履歴を持つエージェント。
type Agent struct {
	client        llm.Client
	model         anthropic.Model
	systemPrompt  string
	tools         []tools.Tool
	toolsByName   map[string]tools.Tool
	perm          *permission.Checker
	ctxmgr        *contextmgr.Manager
	out           io.Writer
	maxIterations int

	// history は会話履歴。Messages APIはステートレスなので、
	// 毎回の呼び出しで履歴全体を送り直す。「会話の記憶」の実体は
	// サーバー側ではなく、このスライスにある。
	history []anthropic.MessageParam

	// lastUsageTokens は直近のAPIレスポンスのトークン使用量
	// (入力+出力)。compaction判定に使う。
	lastUsageTokens int
}

// New はエージェントを作る。
func New(cfg Config) *Agent {
	byName := make(map[string]tools.Tool, len(cfg.Tools))
	for _, t := range cfg.Tools {
		byName[t.Name()] = t
	}
	out := cfg.Out
	if out == nil {
		out = io.Discard
	}
	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}
	ctxMgr := cfg.ContextManager
	if ctxMgr == nil {
		ctxMgr = contextmgr.NewManager()
	}
	perm := cfg.Permission
	if perm == nil {
		perm = permission.New(permission.Config{})
	}
	return &Agent{
		client:        cfg.Client,
		model:         cfg.Model,
		systemPrompt:  cfg.SystemPrompt,
		tools:         cfg.Tools,
		toolsByName:   byName,
		perm:          perm,
		ctxmgr:        ctxMgr,
		out:           out,
		maxIterations: maxIter,
	}
}

// Run は1ユーザーターンを処理する。ツール実行を挟みながらLLMとの
// 往復を繰り返し、最終的なテキスト応答を返す。
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	// compactionはターンの開始時、つまり会話が一区切りついた地点で行う。
	// ツールループの途中で履歴を要約すると、tool_use と tool_result の
	// 対応が壊れてAPIエラーになるため、「どこでcompactするか」は
	// 見た目以上に重要な判断である。
	if a.ctxmgr.ShouldCompact(a.lastUsageTokens) {
		if err := a.CompactHistory(ctx); err != nil {
			return "", err
		}
	}

	a.history = append(a.history, anthropic.NewUserMessage(anthropic.NewTextBlock(userInput)))

	// エージェントループ本体。
	// 「LLMがツールを使い終わって、テキストだけの応答を返してくるまで」
	// が1ユーザーターン。
	for range a.maxIterations {
		resp, err := a.client.Complete(ctx, a.buildParams())
		if err != nil {
			return "", fmt.Errorf("LLM呼び出しに失敗: %w", err)
		}
		fmt.Fprintln(a.out, textOf(resp))

		// アシスタントの応答は tool_use ブロックも含めて丸ごと履歴に積む。
		// tool_use を欠いた履歴に tool_result だけ足すとAPIに拒否される。
		a.history = append(a.history, resp.ToParam())
		a.lastUsageTokens = int(resp.Usage.InputTokens + resp.Usage.OutputTokens)

		// ループを続けるか終わるかの判定。
		//
		// 素朴に書くと `if resp.StopReason != "tool_use" { return }` だが、
		// これは罠がある。stop_reason は tool_use / end_turn の2値ではなく、
		// max_tokens(出力上限で打ち切られた)や refusal(安全上の拒否)も
		// あり、しかも max_tokens では tool_use ブロックが生成途中で
		// 切れていることがある。その応答をそのまま「ターン終了」として
		// 履歴に残すと、tool_use に対応する tool_result が永久に積まれず、
		// 以降のリクエストがすべてAPIに拒否される——会話が死ぬ。
		//
		// そのため判定は「stop_reason」ではなく「未処理のtool_useが
		// 残っているか」を基準にし、stop_reason は続行の可否に使う。
		toolUses := toolUseBlocks(resp)

		if resp.StopReason != anthropic.StopReasonToolUse {
			// ターンは終わり。ただし中断された応答に tool_use が
			// 残っている場合は、打ち切りを伝える tool_result で
			// 必ず対応を閉じてから返す(履歴の整合性を保つ)。
			if len(toolUses) > 0 {
				var results []anthropic.ContentBlockParamUnion
				for _, block := range toolUses {
					results = append(results, anthropic.NewToolResultBlock(block.ID,
						fmt.Sprintf("応答が %s で中断されたため、このツールは実行されませんでした", resp.StopReason), true))
				}
				a.history = append(a.history, anthropic.NewUserMessage(results...))
			}
			if resp.StopReason == anthropic.StopReasonMaxTokens {
				fmt.Fprintln(a.out, "(出力が長すぎて途中で打ち切られました)")
			}
			return textOf(resp), nil
		}

		// stop_reason が tool_use なのに tool_use ブロックが無い、という
		// 矛盾した応答もありうる。空の user メッセージを送るとAPIエラーに
		// なるので、ここで打ち切る。
		if len(toolUses) == 0 {
			return textOf(resp), nil
		}

		// ツールを実行し、結果を1つのuserメッセージにまとめて返す。
		// LLMは1回の応答で複数のツールを呼ぶことがあり(並列ツール呼び出し)、
		// その場合もtool_resultは全部まとめて1メッセージで返すのが作法。
		var results []anthropic.ContentBlockParamUnion
		for _, block := range toolUses {
			results = append(results, a.executeTool(ctx, block))
		}
		a.history = append(a.history, anthropic.NewUserMessage(results...))
	}

	msg := fmt.Sprintf("(%d回の往復上限に達したため打ち切りました)", a.maxIterations)
	fmt.Fprintln(a.out, msg)
	return msg, nil
}

// executeTool は1つの tool_use ブロックを処理し、tool_result ブロックを返す。
//
// 実行前後には「割り込みポイント」が並ぶ。順番に意味がある:
//  1. 権限チェック   — 承認フロー。ユーザーの最終判断
//  2. ツール実行
//  3. 結果の切り詰め — コンテキスト保護
//
// どの段階で失敗しても、エラーは is_error 付きの tool_result として
// LLMに返す。ループを止めないことで、LLMは失敗を踏まえて
// 次の一手を考えられる(これがエージェントの自己修復性の源泉)。
func (a *Agent) executeTool(ctx context.Context, block anthropic.ContentBlockUnion) anthropic.ContentBlockParamUnion {
	fmt.Fprintf(a.out, "⏺ %s(%s)\n", block.Name, compactJSON(string(block.Input)))

	tool, ok := a.toolsByName[block.Name]
	if !ok {
		return anthropic.NewToolResultBlock(block.ID, fmt.Sprintf("ツール %s は存在しません", block.Name), true)
	}

	if err := a.perm.Check(block.Name, block.Input); err != nil {
		fmt.Fprintf(a.out, "  ⨯ %v\n", err)
		return anthropic.NewToolResultBlock(block.ID, err.Error(), true)
	}

	result, err := tool.Run(ctx, block.Input)
	if err != nil {
		fmt.Fprintf(a.out, "  ⨯ %v\n", err)
		return anthropic.NewToolResultBlock(block.ID, err.Error(), true)
	}

	// 空文字のツール結果は tool_result に空のテキストブロックを作り、
	// APIに拒否される。空ファイルを読んだ、出力のないコマンドを実行した、
	// といった正常なケースでも起こるので、ここで必ず埋める。
	// 個々のツールに任せず1箇所で処理するのは、権限チェックと同じ理由。
	if result == "" {
		result = "(空の結果)"
	}

	result = a.ctxmgr.TruncateToolResult(result)
	return anthropic.NewToolResultBlock(block.ID, result, false)
}

// buildParams はAPIリクエストを組み立てる。
func (a *Agent) buildParams() anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: maxTokensPerTurn,
		Messages:  a.history,
	}
	// システムプロンプトが空のときは System 自体を送らない。
	// 空のテキストブロックはAPIに拒否される(text の最小長は1)。
	// 完成形では常に非空だが、章ごとに機能を足していく過程では
	// システムプロンプトがまだ無い段階がある。
	if a.systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: a.systemPrompt}}
	}
	for _, t := range a.tools {
		schema := t.InputSchema()
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name(),
				Description: anthropic.String(t.Description()),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: schema.Properties,
					Required:   schema.Required,
				},
			},
		})
	}
	return params
}

// CompactHistory は履歴を要約に置き換える(自動発動用)。
func (a *Agent) CompactHistory(ctx context.Context) error {
	if len(a.history) == 0 {
		return nil
	}
	fmt.Fprintln(a.out, "(コンテキストを圧縮しています...)")
	// 要約リクエストは通常のターンと同じ形(system・tools付き)で送る。
	// buildParams() をそのまま渡せば、履歴に tool_use が含まれていても
	// リクエストとして矛盾しない。
	compacted, err := a.ctxmgr.Compact(ctx, a.client, a.buildParams())
	if err != nil {
		return err
	}
	a.history = compacted
	a.lastUsageTokens = 0
	return nil
}

// History は現在の会話履歴を返す(テスト・デバッグ用)。
func (a *Agent) History() []anthropic.MessageParam {
	return a.history
}

// toolUseBlocks はレスポンスから tool_use ブロックだけを取り出す。
func toolUseBlocks(msg *anthropic.Message) []anthropic.ContentBlockUnion {
	var blocks []anthropic.ContentBlockUnion
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// textOf はレスポンスからテキストブロックを連結して返す。
func textOf(msg *anthropic.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// compactJSON は表示用に入力JSONを1行・短めに整形する。
//
// 切るのはバイトではなく文字(ルーン)。日本語を含むツール引数を
// バイト数で切ると、端末に文字化けが出る。
func compactJSON(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxDisplayRunes {
		s = string(r[:maxDisplayRunes]) + "..."
	}
	return s
}
