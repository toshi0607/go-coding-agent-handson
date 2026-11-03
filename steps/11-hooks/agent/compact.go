package agent

// コンテキストウィンドウの管理は compact.go と truncate.go が受け持つ。
//
// LLMのコンテキストウィンドウは有限で、会話履歴・ツール結果は
// すべてそこに足されていく。放っておくと:
//  1. 上限超過でAPIエラーになる
//  2. 上限に達しなくても、入力トークン分の課金が毎ターン膨らむ
//  3. 長すぎるコンテキストはレスポンスの品質も下げる
//
// 対策は「入れる量を減らす」(ツール結果の切り詰め。truncate.go)と
// 「足したものを圧縮する」(compaction = 履歴の要約。このファイル)の2本柱。
// どちらもエージェントループの中で使うので、agent パッケージに置いている。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// defaultCompactThreshold はcompactionを始めるトークン数の目安のデフォルト。
// デフォルトのモデル(claude-opus-5-5)の上限100万トークンの半分にしている。
// 要約は早すぎても遅すぎても損をする。早すぎると要約で詳細が失われ、
// 要約の直後はエージェントの判断が悪くなる。遅すぎると、コンテキストが
// 長いほど1リクエストあたりの料金が増える。LangWatchの筆者が自分の
// 利用記録を計測した記事(https://langwatch.ai/blog/context-tax-when-to-compact)では、25万〜45万で
// 要約するのが料金の面でよく、筆者自身は50万〜60万で要約している。
// 上限との差は、判定の後に足されるツールの結果と要約リクエストの分の余裕になる。
const defaultCompactThreshold = 500_000

// shouldCompact は直近のAPIレスポンスのトークン使用量から、
// compactionすべきかを判定する。
//
// トークン数を自前で数えるのは難しい(トークナイザーはモデル依存)ので、
// APIレスポンスの usage(実測値)を使う。これが最も正確で、かつタダである。
func (a *Agent) shouldCompact() bool {
	return a.lastUsageTokens > a.compactThreshold
}

// compactIfNeeded は、閾値を超えていれば会話履歴を要約に置き換える。
//
// 要約に失敗しても、会話履歴はそのまま残っているので、警告を表示して
// ターンを続ける。失敗のたびにターンを止めると、次のターンも閾値を
// 超えたまま要約に失敗し、ユーザーは会話を先に進められなくなる。
// 失敗したターンの残りでは試し直さず、次のターンの冒頭で試し直す。
// 閾値を超えたままなので、試し直すとツールを呼ぶたびに要約の
// リクエストが増える。Ctrl-C でキャンセルされたときだけエラーを返す。
func (a *Agent) compactIfNeeded(ctx context.Context) error {
	if a.compactFailed || !a.shouldCompact() {
		return nil
	}
	if err := a.Compact(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintf(a.out, "  ⨯ %v。このターンは圧縮せずに続けます\n", err)
		a.compactFailed = true
	}
	return nil
}

const summaryPrompt = `これまでの会話を、この続きから作業を再開するために必要な情報だけに要約してください。以下を必ず含めること:
- ユーザーの依頼内容と現在の進捗
- 判明した重要な事実(ファイルパス、コードの構造、エラーの内容など)
- 未完了のタスクと次にやるべきこと
要約以外の文章(前置きなど)は不要です。`

// summaryMaxTokens は要約1回分の出力上限。デフォルトのモデルでは思考の分もここに数えられる。
const summaryMaxTokens = 8192

// Compact は会話履歴を要約して置き換える(/compact コマンドと自動発動用)。
//
// やることはシンプルで、「履歴の最後に要約依頼を足してLLMに投げ、
// 返ってきた要約だけを新しい履歴にする」。要約もまた、LLM呼び出しである。
//
// ここでの設計判断は「何を残すか」:
//   - 全部要約で置き換える(この実装。最も単純)
//   - 直近Nターンは生のまま残し、それ以前だけ要約する
//   - ツール結果だけ削り、会話文は残す
//
// 下に行くほど直近の文脈は保たれるが、実装は複雑になる。
// 何が失われるかを意識して選ぶこと。要約は非可逆圧縮であり、
// 一度失った詳細は戻らない。
//
// 要約リクエストは通常のターンと同じ形(buildParams() の結果)で送る。
// 要約のためだけに、ツールの定義を外したリクエストを別に組み立てずに済む。
// なお、履歴に tool_use / tool_result ブロックが含まれていても、
// ツールの定義のないリクエストをMessages APIは受け付けた
// (2026年10月、claude-haiku-5-5 と claude-opus-5-5 で確認)。
func (a *Agent) Compact(ctx context.Context) error {
	if len(a.history) == 0 {
		return nil
	}
	fmt.Fprintln(a.out, "(コンテキストを圧縮しています...)")

	params := a.buildParams()
	params.MaxTokens = summaryMaxTokens
	// params.Messages は a.history と配列を共有しているので、append は
	// a.history の配列の空き(長さより後ろ)に書き込むことがある。
	// a.history の長さは変わらず、成功すれば直後に丸ごと置き換えるので、
	// 会話履歴は壊れない。
	// ツールループの途中で呼ばれたときは、履歴が tool_result の
	// userメッセージで終わっているので、userメッセージが2つ続く。
	// Messages APIは同じroleの連続したメッセージを1つにまとめて扱う。
	params.Messages = append(params.Messages,
		anthropic.NewUserMessage(anthropic.NewTextBlock(summaryPrompt)))
	// tools を渡す以上、LLMは要約の代わりにツールを呼ぶ選択もできてしまう。
	// ここで欲しいのはテキストの要約だけなので、tool_choice で封じる。
	noTools := anthropic.NewToolChoiceNoneParam()
	params.ToolChoice = anthropic.ToolChoiceUnionParam{OfNone: &noTools}

	resp, err := a.client.Complete(ctx, params)
	if err != nil {
		return fmt.Errorf("compaction用の要約に失敗しました: %w", err)
	}

	var summary strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			summary.WriteString(block.Text)
		}
	}
	// 空の要約で置き換えると、ここまでの会話がすべて失われる。
	// 要約に失敗したときと同じく、会話履歴を残してエラーを返す。
	if strings.TrimSpace(summary.String()) == "" {
		return errors.New("compaction用の要約が空でした")
	}

	// 要約を「ユーザーからの引き継ぎメッセージ」として新しい履歴の先頭に置く。
	a.history = []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(
			"(コンテキスト圧縮により、ここまでの会話は以下の要約に置き換えられました)\n\n" + summary.String())),
	}
	a.lastUsageTokens = 0
	return nil
}
