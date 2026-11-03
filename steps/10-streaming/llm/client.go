// Package llm はLLM(Messages API)呼び出しを受け持つインターフェースを定義する。
//
// エージェント本体は Client インターフェースにだけ依存する。
// これにより:
//   - 本番は AnthropicClient(実API)
//   - テストは ReplayClient(録画したレスポンスの再生。APIキー不要)
//
// を差し替えられる。リクエスト/レスポンスの型はSDKのものをそのまま使う。
// 独自型にラップすると「Messages APIが実際どういう形をしているか」が
// 見えなくなってしまうので、教材としてはあえて生のまま扱う。
package llm

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

// Client はMessages APIの呼び出し口。
type Client interface {
	// Complete は1回のAPI呼び出しを行い、完全なレスポンスを返す。
	Complete(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)

	// Stream はストリーミングでAPIを呼び出す。
	// テキストの断片が届くたびに onText を呼び(nil可)、
	// 最終的に蓄積された完全なレスポンスを返す。
	// 会話履歴に足すのは戻り値の完全なメッセージであり、
	// onText はあくまで表示用という役割分担に注意。
	Stream(ctx context.Context, params anthropic.MessageNewParams, onText func(string)) (*anthropic.Message, error)
}
