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
	"fmt"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// defaultCompactThreshold はcompactionを始めるトークン数の目安のデフォルト。
// モデルの上限(20万〜100万)よりだいぶ手前に設定する。上限ギリギリまで
// 使うと、要約リクエスト自体が入らなくなるうえ、品質劣化も先に来る。
const defaultCompactThreshold = 80_000

// shouldCompact は直近のAPIレスポンスのトークン使用量から、
// compactionすべきかを判定する。
//
// トークン数を自前で数えるのは難しい(トークナイザーはモデル依存)ので、
// APIレスポンスの usage(実測値)を使う。これが最も正確で、かつタダである。
func (a *Agent) shouldCompact() bool {
	return a.lastUsageTokens > a.compactThreshold
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
// 要約だからといって Messages 以外を省いてはいけない:
// 圧縮対象の履歴には tool_use / tool_result ブロックが含まれており、
// tools を伴わないリクエストでそれらを送るとAPIに拒否される。
// 「要約用の軽いリクエスト」を別途組み立てたくなるが、履歴の中身と
// リクエストのパラメーターには対応関係があり、勝手に外せない。
func (a *Agent) Compact(ctx context.Context) error {
	if len(a.history) == 0 {
		return nil
	}
	fmt.Fprintln(a.out, "(コンテキストを圧縮しています...)")

	params := a.buildParams()
	params.MaxTokens = summaryMaxTokens
	// a.history と配列を共有したまま append すると履歴を
	// 破壊しうるので、必ず複製してから足す。
	params.Messages = append(slices.Clone(params.Messages),
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

	// 要約を「ユーザーからの引き継ぎメッセージ」として新しい履歴の先頭に置く。
	a.history = []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(
			"(コンテキスト圧縮により、ここまでの会話は以下の要約に置き換えられました)\n\n" + summary.String())),
	}
	a.lastUsageTokens = 0
	return nil
}
