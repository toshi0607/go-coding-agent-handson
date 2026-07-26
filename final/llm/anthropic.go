package llm

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

// AnthropicClient は anthropic-sdk-go を使った Client の実装。
type AnthropicClient struct {
	client anthropic.Client
}

// NewAnthropicClient は環境変数 ANTHROPIC_API_KEY から認証情報を読む
// クライアントを作る。
func NewAnthropicClient() *AnthropicClient {
	return &AnthropicClient{client: anthropic.NewClient()}
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
	message := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return nil, err
		}
		if onText == nil {
			continue
		}
		switch eventVariant := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			if delta, ok := eventVariant.Delta.AsAny().(anthropic.TextDelta); ok {
				onText(delta.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return &message, nil
}
