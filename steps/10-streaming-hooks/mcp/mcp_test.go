package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// テスト用のMCPサーバーは SDK のサーバー機能で作る。
// 手書きの疑似サーバーではなく本物の実装を相手にするので、
// プロトコルの細部(バージョンの交渉など)まで実際のやりとりになる。

type weatherInput struct {
	City string `json:"city" jsonschema:"都市名"`
}

// newTestServer は次の3つのツールを持つサーバーを作る。
//   - get_weather: 都市名を受け取って天気を返す
//   - broken:      常にツールの失敗(isError)を返す
//   - slow:        キャンセルを無視して、しばらくしてから結果を返す
func newTestServer() *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "weather", Version: "v0.1.0"}, nil)
	sdk.AddTool(s, &sdk.Tool{Name: "get_weather", Description: "天気を返す"},
		func(ctx context.Context, req *sdk.CallToolRequest, in weatherInput) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "晴れ (" + in.City + ")"}}}, nil, nil
		})
	sdk.AddTool(s, &sdk.Tool{Name: "broken", Description: "常に失敗する"},
		func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{
				IsError: true,
				Content: []sdk.Content{&sdk.TextContent{Text: "APIキーがありません"}},
			}, nil, nil
		})
	sdk.AddTool(s, &sdk.Tool{Name: "slow", Description: "応答が遅い"},
		func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
			// 行儀の悪いサーバーの再現。キャンセルの通知が来ても止まらず、
			// 諦められた後に結果を返す。
			time.Sleep(2 * callTimeout)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "遅れた結果"}}}, nil, nil
		})
	return s
}

// connectInMemory はメモリ上のサーバーに繋いだクライアントを返す。
func connectInMemory(t *testing.T) *Client {
	t.Helper()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := newTestServer().Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("サーバーの起動: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	c, err := connect(t.Context(), "weather", clientTransport)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// shortenCallTimeout はテストの間だけ応答待ちの上限を短くする。
func shortenCallTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := callTimeout
	callTimeout = d
	t.Cleanup(func() { callTimeout = old })
}

// findTool はツール一覧から名前で1つ取り出す。
func findTool(t *testing.T, c *Client, name string) *Tool {
	t.Helper()
	list, err := c.Tools(t.Context())
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	for _, tool := range list {
		if tool.Name() == name {
			return tool.(*Tool)
		}
	}
	t.Fatalf("ツール %s がありません", name)
	return nil
}

// サーバーのツールが、名前空間付きの名前と、LLMに渡せるスキーマを持つ
// tools.Tool になること。
func TestToolsWrapsServerTools(t *testing.T) {
	c := connectInMemory(t)
	list, err := c.Tools(t.Context())
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ツールの数: got %d, want 3", len(list))
	}

	tool := findTool(t, c, "mcp__weather__get_weather")
	if got := tool.Description(); got != "天気を返す" {
		t.Errorf("Description: got %q", got)
	}
	schema := tool.InputSchema()
	city, ok := schema.Properties["city"].(map[string]any)
	if !ok || city["type"] != "string" {
		t.Errorf("properties.city: %v", schema.Properties)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "city" {
		t.Errorf("required: %v", schema.Required)
	}
}

// LLMが付けた名前空間を外し、サーバー上の名前でツールを呼ぶこと。
func TestRunCallsServerTool(t *testing.T) {
	c := connectInMemory(t)
	tool := findTool(t, c, "mcp__weather__get_weather")

	got, err := tool.Run(t.Context(), []byte(`{"city": "Tokyo"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "晴れ (Tokyo)" {
		t.Errorf("got %q", got)
	}
}

// ツールの失敗(isError)は error になり、LLMに理由が伝わること。
func TestRunReturnsToolError(t *testing.T) {
	c := connectInMemory(t)
	tool := findTool(t, c, "mcp__weather__broken")

	_, err := tool.Run(t.Context(), []byte(`{}`))
	if err == nil {
		t.Fatal("isError の結果がエラーになっていない")
	}
	if !strings.Contains(err.Error(), "APIキーがありません") {
		t.Errorf("サーバーが返した理由がエラーに含まれていない: %v", err)
	}
}

// 応答しないツールを待ち続けず、上限で諦めること。
// 諦めた呼び出しの結果が後から届いても、次の呼び出しが正しい結果を受け取ること
// (id の突き合わせは SDK の仕事だが、ずれていないことはここで確かめる)。
func TestRunTimesOutOnSlowTool(t *testing.T) {
	shortenCallTimeout(t, 200*time.Millisecond)
	c := connectInMemory(t)
	slow := findTool(t, c, "mcp__weather__slow")

	start := time.Now()
	_, err := slow.Run(t.Context(), []byte(`{}`))
	if err == nil {
		t.Fatal("応答しないツールでエラーになっていない")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("期限切れと判別できない: %v", err)
	}
	if !strings.Contains(err.Error(), "slow") {
		t.Errorf("どのツールが応答しないか分からない: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("上限で諦めていない: %v", elapsed)
	}

	// slow の結果が届く頃まで待ってから、別のツールを呼ぶ。
	time.Sleep(2 * callTimeout)
	weather := findTool(t, c, "mcp__weather__get_weather")
	got, err := weather.Run(t.Context(), []byte(`{"city": "Osaka"}`))
	if err != nil {
		t.Fatalf("次の呼び出し: %v", err)
	}
	if got != "晴れ (Osaka)" {
		t.Errorf("次の呼び出しが別の結果を受け取った: %q", got)
	}
}

// Ctrl-C(呼び出し元のキャンセル)で待つのをやめること。
func TestRunStopsOnCancel(t *testing.T) {
	shortenCallTimeout(t, time.Second)
	c := connectInMemory(t)
	slow := findTool(t, c, "mcp__weather__slow")

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := slow.Run(ctx, []byte(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("キャンセルで止まっていない: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= callTimeout {
		t.Errorf("キャンセルではなく上限で止まっている: %v", elapsed)
	}
}

// ここから下は本物の子プロセスを相手にした検証。
// メモリ上のサーバーでは、子プロセスの起動・標準エラー出力の転送・
// 終了しないプロセスの強制終了が通らないので、実プロセスで確かめる。

// helperEnv が設定されていると、テストのバイナリ自身がMCPサーバーとして動く。
const helperEnv = "MCP_TEST_HELPER_SERVER"

func init() {
	if os.Getenv(helperEnv) == "" {
		return
	}
	fmt.Fprintln(os.Stderr, "起動しました")
	// 標準入力が閉じられると Run が返る(stdio transport の作法)。
	if err := newTestServer().Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// 子プロセスとして起動したサーバーのツールを呼べること。
// サーバーの標準エラー出力が接頭辞付きで転送され、送受信したメッセージが
// Log に書き出され、Close で正しく終了を待つこと。
func TestConnectToSubprocess(t *testing.T) {
	t.Setenv(helperEnv, "1")
	var stderr, log bytes.Buffer
	c, err := Connect(t.Context(), Server{
		Name:    "helper",
		Command: os.Args[0],
		Stderr:  &stderr,
		Log:     &log,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	tool := findTool(t, c, "mcp__helper__get_weather")
	got, err := tool.Run(t.Context(), []byte(`{"city": "Sapporo"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "晴れ (Sapporo)" {
		t.Errorf("got %q", got)
	}

	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("正常終了するサーバーでエラー: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close が返ってこない")
	}

	// Close は子プロセスの終了を待つので、ここで読んでも競合しない。
	if got := stderr.String(); got != "[mcp:helper] 起動しました\n" {
		t.Errorf("サーバーのログ: got %q", got)
	}
	for _, method := range []string{"server/discover", "tools/list", "tools/call"} {
		if !strings.Contains(log.String(), `"method":"`+method+`"`) {
			t.Errorf("Log に %s がありません:\n%s", method, log.String())
		}
	}
	// サーバーからの問い合わせには答えないので、capabilities は空で宣言する。
	if !strings.Contains(log.String(), `"io.modelcontextprotocol/clientCapabilities":{}`) {
		t.Errorf("capabilities が空で宣言されていません:\n%s", log.String())
	}
}

// discoverReply は server/discover にだけ応答するシェルスクリプトの断片。
const discoverReply = `read -r line; printf '{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete","supportedVersions":["2026-07-28"],"capabilities":{"tools":{}}}}\n'; `

// 接続時の問い合わせに応答しないサーバーで、起動が止まらないこと。
func TestConnectTimesOutOnSilentServer(t *testing.T) {
	shortenCallTimeout(t, 300*time.Millisecond)
	start := time.Now()
	_, err := Connect(t.Context(), Server{
		Name:    "silent",
		Command: "sh",
		// 何も返さずに居座る。exec で置き換えるのは、シェルを止めても
		// 子の sleep が生き残るのを避けるため。
		Args: []string{"-c", "exec sleep 60"},
	})
	if err == nil {
		t.Fatal("応答しないサーバーに接続できてしまった")
	}
	if !strings.Contains(err.Error(), "silent") {
		t.Errorf("どのサーバーか分からない: %v", err)
	}
	if elapsed := time.Since(start); elapsed > shutdownTimeout+10*time.Second {
		t.Errorf("接続を諦めるまでが長すぎる: %v", elapsed)
	}
}

// 標準入力を閉じても終了しないサーバーを、待ち続けずに止めること。
// この経路がないと、行儀の悪いサーバー1つでエージェントが終了できなくなる。
func TestCloseStopsUnresponsiveServer(t *testing.T) {
	c, err := Connect(t.Context(), Server{
		Name:    "stubborn",
		Command: "sh",
		Args:    []string{"-c", discoverReply + "exec sleep 60"},
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case <-done:
		// 止めたので Close はエラー(signal: terminated)を返す。内容は問わない。
	case <-time.After(shutdownTimeout + 10*time.Second):
		t.Fatal("終了しないサーバーで Close が固まった")
	}
	if elapsed := time.Since(start); elapsed < shutdownTimeout {
		t.Errorf("待たずに止めている: %v", elapsed)
	}
}

// サーバーの標準エラー出力に行の途中で分割して届いても、
// 1行に1つだけ接頭辞が付くこと。
func TestPrefixWriter(t *testing.T) {
	var buf bytes.Buffer
	w := &prefixWriter{w: &buf, prefix: "[mcp:weather] "}

	for _, chunk := range []string{"起動し", "ました\n設定を", "読み込み中\n途中まで"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}

	want := "[mcp:weather] 起動しました\n[mcp:weather] 設定を読み込み中\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
