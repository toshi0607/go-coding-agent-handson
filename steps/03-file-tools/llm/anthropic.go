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
