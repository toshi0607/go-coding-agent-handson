package llm

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient は anthropic-sdk-go を使った Client の実装。
type AnthropicClient struct {
	client anthropic.Client
}

// NewAnthropicClient は環境変数 ANTHROPIC_API_KEY から認証情報を読む
// クライアントを作る。
//
// opts はSDKに渡す追加設定。通常は不要だが、テストで接続先を
// 差し替える(実APIを叩かずにストリーミング処理を検証する)ために
// 受け口だけ開けてある。
func NewAnthropicClient(opts ...option.RequestOption) *AnthropicClient {
	return &AnthropicClient{client: anthropic.NewClient(opts...)}
}

func (c *AnthropicClient) Complete(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	return c.client.Messages.New(ctx, params)
}

func (c *AnthropicClient) Stream(ctx context.Context, params anthropic.MessageNewParams, onText func(string)) (*anthropic.Message, error) {
	stream := c.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	// TODO(step10-1): ストリーミングの受信処理を実装する。
	//
	// ストリーミングではレスポンスが SSE イベントの列に分解されて届く:
	// message_start → content_block_start → content_block_delta... → message_stop
	//
	// 満たすべき仕様:
	//  - stream.Next() で回し、各イベント(stream.Current())を
	//    anthropic.Message に蓄積する。SDKの Message.Accumulate が
	//    イベントから完全なメッセージを組み立て直してくれる
	//  - テキストの差分が届いたら onText に流す(onText は nil でもよい)。
	//    イベントが ContentBlockDeltaEvent で、その Delta が TextDelta の
	//    ときだけがテキスト差分である(AsAny() で型を判別できる。
	//    ツール引数の断片 input_json_delta などは流さない)
	//  - ループ後に stream.Err() を確認する
	//  - 蓄積した完全な Message を返す。会話履歴に積むのはこの戻り値で、
	//    onText はあくまで表示用——という役割分担が肝である
	panic("TODO(step10-1): steps/10-streaming-hooks/llm/anthropic.go を実装してください(hints.md 参照)")
}
