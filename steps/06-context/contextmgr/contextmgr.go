// Package contextmgr はコンテキストウィンドウの管理を担う。
//
// LLMのコンテキストウィンドウは有限で、会話履歴・ツール結果は
// すべてそこに積まれていく。放っておくと:
//  1. 上限超過でAPIエラーになる
//  2. 上限に達しなくても、入力トークン分の課金が毎ターン膨らむ
//  3. 長すぎるコンテキストは応答品質も下げる
//
// 対策は「入れる量を減らす」(ツール結果の切り詰め)と
// 「積んだものを圧縮する」(compaction = 履歴の要約)の2本柱。
package contextmgr

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/06-context/llm"
)

const (
	// DefaultCompactThreshold はcompactionを始めるトークン数の目安。
	// モデルの上限(20万〜100万)よりだいぶ手前に設定する。上限ギリギリまで
	// 使うと、要約リクエスト自体が入らなくなるうえ、品質劣化も先に来る。
	DefaultCompactThreshold = 80_000

	// DefaultMaxToolResultChars はツール結果1件あたりの文字数上限。
	// 巨大ファイルの read_file やビルドログの暴発からコンテキストを守る。
	DefaultMaxToolResultChars = 20_000
)

// Manager はコンテキスト管理の設定と操作をまとめたもの。
type Manager struct {
	// CompactThreshold を超えるトークン数を観測したらcompactionを行う。
	CompactThreshold int
	// MaxToolResultChars を超えるツール結果は切り詰める。
	MaxToolResultChars int
}

// NewManager はデフォルト設定の Manager を作る。
func NewManager() *Manager {
	return &Manager{
		CompactThreshold:   DefaultCompactThreshold,
		MaxToolResultChars: DefaultMaxToolResultChars,
	}
}

// TruncateToolResult はツール結果が長すぎる場合に切り詰める。
//
// TODO(step06-1): 実装する。満たすべき仕様:
//
//  - MaxToolResultChars 文字以内ならそのまま返す
//  - 超えていたら「先頭 MaxToolResultChars 文字」を残して後ろを捨てる。
//    エラーメッセージやファイルの重要情報は先頭に現れることが多い
//    ためだが、これは唯一の正解ではない。テストログは末尾にこそ結果が
//    出るので「先頭+末尾を残す」戦略もありうるし、実際のエージェントには
//    要約をかけるものもある。何をどう捨てるかは設計の立派な判断ポイントだ
//  - 切り詰めたことと全体の長さを、切り詰めた結果の末尾に書き足して
//    LLMに必ず伝える。黙って切ると、LLMは「全部読めた」と誤解した
//    まま推論を進めてしまう
//  - 数えるのはバイトではなく文字(ルーン)。下の truncateRunes を
//    実装して使うこと
func (m *Manager) TruncateToolResult(result string) string {
	panic("TODO(step06-1): steps/06-context/contextmgr/contextmgr.go を実装してください(hints.md 参照)")
}

// truncateRunes は s の先頭 max 文字と、s 全体の文字数を返す。
//
// TODO(step06-1続き): 実装する。
//
// Goの文字列はUTF-8のバイト列である。`s[:20000]` と書くと日本語や
// ソースコード中のマルチバイト文字の途中で切れ、壊れたバイト列が
// そのままAPIに送られる。「N文字で切り詰めた」と告知しながらバイト数で
// 切るのは、日本語を扱うツールでは実害のある嘘になる。
//
// []rune(s) に変換すれば文字単位で切れるが、ツール結果は数MBに
// なりうるので全体の変換コピーは避けたい。range over string が
// 「ルーンの開始バイト位置」を返すことを利用すると、コピーなしで
// バイト境界とルーン境界を一致させられる。
func truncateRunes(s string, max int) (head string, total int) {
	panic("TODO(step06-1続き): steps/06-context/contextmgr/contextmgr.go を実装してください(hints.md 参照)")
}

// ShouldCompact は直近のAPIレスポンスのトークン使用量から、
// compactionすべきかを判定する。
//
// トークン数を自前で数えるのは難しい(トークナイザはモデル依存)ので、
// APIレスポンスの usage(実測値)を使う。これが最も正確で、かつタダである。
func (m *Manager) ShouldCompact(lastUsageTokens int) bool {
	return lastUsageTokens > m.CompactThreshold
}

const summaryPrompt = `これまでの会話を、この続きから作業を再開するために必要な情報だけに要約してください。以下を必ず含めること:
- ユーザーの依頼内容と現在の進捗
- 判明した重要な事実(ファイルパス、コードの構造、エラーの内容など)
- 未完了のタスクと次にやるべきこと
要約以外の文章(前置きなど)は不要です。`

// summaryMaxTokens は要約1回分の出力上限。
const summaryMaxTokens = 4096

// Compact は会話履歴を要約して置き換える。
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
// base には通常のターンと同じリクエスト(agent.buildParams() の結果)を
// そのまま渡す。要約だからといって Messages 以外を省いてはいけない:
// 圧縮対象の履歴には tool_use / tool_result ブロックが含まれており、
// tools を伴わないリクエストでそれらを送るとAPIに拒否される。
// 「要約用の軽いリクエスト」を別途組み立てたくなるが、履歴の中身と
// リクエストのパラメータには対応関係があり、勝手に外せない。
func (m *Manager) Compact(ctx context.Context, client llm.Client, base anthropic.MessageNewParams) ([]anthropic.MessageParam, error) {
	params := base
	params.MaxTokens = summaryMaxTokens
	// 元のスライスを共有したまま append すると呼び出し側の履歴を
	// 破壊しうるので、必ず複製してから足す。
	params.Messages = append(slices.Clone(base.Messages),
		anthropic.NewUserMessage(anthropic.NewTextBlock(summaryPrompt)))
	// tools を渡す以上、LLMは要約の代わりにツールを呼ぶ選択もできてしまう。
	// ここで欲しいのはテキストの要約だけなので、tool_choice で封じる。
	noTools := anthropic.NewToolChoiceNoneParam()
	params.ToolChoice = anthropic.ToolChoiceUnionParam{OfNone: &noTools}

	resp, err := client.Complete(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("compaction用の要約に失敗: %w", err)
	}

	var summary strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			summary.WriteString(block.Text)
		}
	}

	// 要約を「ユーザーからの引き継ぎメッセージ」として新しい履歴の先頭に置く。
	return []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(
			"(コンテキスト圧縮により、ここまでの会話は以下の要約に置き換えられました)\n\n" + summary.String())),
	}, nil
}
