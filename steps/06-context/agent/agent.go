// Package agent はエージェントの本体、すなわち「ループ」を実装する。
//
// エージェントの正体は次のループである:
//
//	ユーザー入力 → LLM呼び出し → (ツール実行 → 結果を返してLLM再呼び出し)* → レスポンス
//
// LLMは「次にどのツールを使うか」を決めるだけで、実際に手を動かす
// (ファイルを読む、コマンドを実行する)のはこちら側のコードである。
// この役割分担と、その間を往復するメッセージの足し方さえ理解すれば、
// コーディングエージェントの骨格は理解できたことになる。
package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/permission"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/tools"
)

const (
	// maxTokensPerTurn は1回のLLM呼び出しの出力上限。
	// デフォルトのモデルは常に思考(thinking)し、その分もこの上限に数えられるので、
	// 見えるレスポンスの長さだけで決めると足りなくなる。一方、ストリーミングしない
	// 呼び出しでもHTTPタイムアウトに収まるよう、大きくしすぎてもいない。
	maxTokensPerTurn = 16000

	// maxDisplayRunes はツール実行を画面に表示するときの引数の文字数上限。
	maxDisplayRunes = 120

	// defaultMaxIterations は1ターンでLLMを呼ぶ回数の上限。
	// LLMがツールを呼び続ける限りループは回るので、暴走(同じ失敗を
	// 延々と繰り返す等)への保険として上限を置く。上限に達したら
	// ターンを終えてユーザーに制御を返す。
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
	// CompactThreshold は自動compactionを始めるトークン数(0ならデフォルト)。
	CompactThreshold int
	// MaxToolResultChars はツール結果1件あたりの文字数上限(0ならデフォルト)。
	MaxToolResultChars int
	// Out は途中経過(ストリーミングテキスト、ツール実行表示)の出力先。
	Out io.Writer
	// MaxIterations は1ターンでLLMを呼ぶ回数の上限(0ならデフォルト)。
	MaxIterations int
}

// Agent は会話履歴を持つエージェント。
type Agent struct {
	client             llm.Client
	model              anthropic.Model
	systemPrompt       string
	tools              []tools.Tool
	toolsByName        map[string]tools.Tool
	perm               *permission.Checker
	compactThreshold   int
	maxToolResultChars int
	out                io.Writer
	maxIterations      int

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
	compactThreshold := cfg.CompactThreshold
	if compactThreshold <= 0 {
		compactThreshold = defaultCompactThreshold
	}
	maxResultChars := cfg.MaxToolResultChars
	if maxResultChars <= 0 {
		maxResultChars = defaultMaxToolResultChars
	}
	perm := cfg.Permission
	if perm == nil {
		perm = permission.New(permission.Config{})
	}
	return &Agent{
		client:             cfg.Client,
		model:              cfg.Model,
		systemPrompt:       cfg.SystemPrompt,
		tools:              cfg.Tools,
		toolsByName:        byName,
		perm:               perm,
		compactThreshold:   compactThreshold,
		maxToolResultChars: maxResultChars,
		out:                out,
		maxIterations:      maxIter,
	}
}

// Run は1ユーザーターンを処理する。ツール実行を挟みながらLLMとの
// 往復を繰り返し、最終的なテキストのレスポンスを返す。
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	// TODO(step06-2): 自動compactionを発動させる(2か所。1か所目はここ)。
	//
	// 直近のトークン使用量(a.lastUsageTokens)が閾値を超えていたら
	// (a.shouldCompact)、履歴を圧縮する(a.Compact。
	// エラーはそのまま返してよい)。
	//
	// このstepの主題は「圧縮のコードを書くこと」ではなく「どこで
	// 圧縮するか」である。圧縮してよいのは tool_use と tool_result の
	// 対応が閉じている地点だけで、ここ(ターンの開始時)と、下のツール
	// ループで tool_result を履歴に足した直後の2か所がそれにあたる。
	// 対応が閉じていない地点で圧縮すると何が壊れるかは README と
	// TestCompactionHappensOnlyWherePairsAreClosed を参照。

	a.history = append(a.history, anthropic.NewUserMessage(anthropic.NewTextBlock(userInput)))

	// エージェントループ本体。
	// 「LLMがツールを使い終わって、テキストだけのレスポンスを返してくるまで」
	// が1ユーザーターン。
	for range a.maxIterations {
		resp, err := a.client.Complete(ctx, a.buildParams())
		if err != nil {
			return "", fmt.Errorf("LLMの呼び出しに失敗しました: %w", err)
		}
		fmt.Fprintln(a.out, textOf(resp))

		// アシスタントのレスポンスは tool_use ブロックも含めて丸ごと履歴に足す。
		// tool_use を欠いた履歴に tool_result だけ足すとAPIに拒否される。
		a.history = append(a.history, resp.ToParam())
		a.lastUsageTokens = int(resp.Usage.InputTokens + resp.Usage.OutputTokens)

		// ループを続けるか終わるかの判定。
		//
		// 素朴に書くと `if resp.StopReason != "tool_use" { return }` だが、
		// これは罠がある。stop_reason は tool_use / end_turn の2値ではなく、
		// max_tokens(出力上限で打ち切られた)や refusal(安全上の拒否)も
		// あり、しかも max_tokens では tool_use ブロックが生成途中で
		// 切れていることがある。そのレスポンスをそのまま「ターン終了」として
		// 履歴に残すと、tool_use に対応する tool_result が永久に足されず、
		// 会話履歴を消すまで以降のリクエストがすべてAPIに拒否される。
		//
		// そのため判定は「stop_reason」ではなく「未処理のtool_useが
		// 残っているか」を基準にし、stop_reason は続行の可否に使う。
		toolUses := toolUseBlocks(resp)

		if resp.StopReason != anthropic.StopReasonToolUse {
			// ターンは終わり。ただし中断されたレスポンスに tool_use が
			// 残っている場合は、打ち切りを伝える tool_result で
			// 必ず対応を閉じてから返す(履歴の整合性を保つ)。
			if len(toolUses) > 0 {
				var results []anthropic.ContentBlockParamUnion
				for _, block := range toolUses {
					results = append(results, anthropic.NewToolResultBlock(block.ID,
						fmt.Sprintf("レスポンスが %s で中断されたため、このツールは実行されませんでした", resp.StopReason), true))
				}
				a.history = append(a.history, anthropic.NewUserMessage(results...))
			}
			if resp.StopReason == anthropic.StopReasonMaxTokens {
				fmt.Fprintln(a.out, "(出力がmax_tokensの上限に達したため、レスポンスが途中で終わっています)")
			}
			return textOf(resp), nil
		}

		// stop_reason が tool_use なのに tool_use ブロックが無い、という
		// 矛盾したレスポンスもありうる。空の user メッセージを送るとAPIエラーに
		// なるので、ここで打ち切る。
		if len(toolUses) == 0 {
			return textOf(resp), nil
		}

		// ツールを実行し、結果を1つのuserメッセージにまとめて返す。
		// LLMは1回のレスポンスで複数のツールを呼ぶことがあり(並列ツール呼び出し)、
		// その場合もtool_resultは全部まとめて1メッセージで返すのが作法。
		var results []anthropic.ContentBlockParamUnion
		for _, block := range toolUses {
			results = append(results, a.executeTool(ctx, block))
		}
		a.history = append(a.history, anthropic.NewUserMessage(results...))

		// TODO(step06-2続き): 2か所目。tool_result を履歴に足した直後にも、
		// ターンの開始時と同じ判定と圧縮を行う。
	}

	msg := fmt.Sprintf("(LLMの呼び出しが上限の%d回に達したため、ターンを終了しました)", a.maxIterations)
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
		// 失敗したビルドのログのように、エラーも長くなることがあるので切り詰める。
		return anthropic.NewToolResultBlock(block.ID, truncateToolResult(err.Error(), a.maxToolResultChars), true)
	}

	// 空文字のツール結果は、空のままでも Messages API は受け付けるが、
	// ツールが成功して出力が無かったことをLLMが文として読み取れるように、
	// 文字列に置き換える。空ファイルを読んだ、出力のないコマンドを実行した、
	// といった正常なケースで起こる。
	// 個々のツールに任せず1箇所で処理するのは、権限チェックと同じ理由。
	if result == "" {
		result = "(ツールの出力は空です)"
	}

	result = truncateToolResult(result, a.maxToolResultChars)
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
