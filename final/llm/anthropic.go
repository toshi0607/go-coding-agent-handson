package llm

import (
	"context"
	"fmt"
	"io"

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

	// ストリーミングではレスポンスがイベントの列に分解されて届く。
	// message_start → content_block_start → content_block_delta... → message_stop
	// Accumulate がイベントを完全な Message に組み立て直してくれるので、
	// こちらはテキスト差分(text_delta)を表示に流すことだけ考えればよい。
	var message anthropic.Message
	stopped := false
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return nil, err
		}
		switch eventVariant := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			if delta, ok := eventVariant.Delta.AsAny().(anthropic.TextDelta); ok && onText != nil {
				onText(delta.Text)
			}
		case anthropic.MessageStopEvent:
			stopped = true
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	// 接続が途中で切れても、SSEとしては正常に終わったように見えることがある。
	// そのとき stream.Err() は nil なので、最後のイベントの message_stop が
	// 届いたかどうかで見分ける。届いていなければレスポンスは未完成である。
	if !stopped {
		return nil, fmt.Errorf("message_stop を受け取る前にストリームが終わりました: %w", io.ErrUnexpectedEOF)
	}
	return &message, nil
}
