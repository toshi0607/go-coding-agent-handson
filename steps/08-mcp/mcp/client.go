// Package mcp はMCP(Model Context Protocol)のクライアントを実装する。
//
// MCPは、ツールを外部プロセス(MCPサーバー)として提供するための標準
// プロトコルである。stdio transport では、サーバーを子プロセスとして
// 起動し、標準入出力で改行区切りのJSON-RPC 2.0メッセージを交換する。
// このクライアントが使うメソッドは3つ:
//   - server/discover: サーバーが対応するプロトコルのバージョンと
//     capabilities を問い合わせる
//   - tools/list:      サーバーが提供するツールの一覧を取得
//   - tools/call:      ツールを実行
//
// プロトコルの処理は公式Go SDK(github.com/modelcontextprotocol/go-sdk)に
// 任せる。JSON-RPCメッセージの読み書き、リクエストとレスポンスの id の
// 突き合わせ、プロトコルのバージョンの交渉は SDK の仕事である。
// このパッケージが自分で書くのは次の2つ:
//   - MCPのツールを tools.Tool として扱えるようにするアダプタ(tool.go)
//   - 外部プロセスに対する最低限の備え(応答待ちの上限、終了しない
//     サーバーの強制終了、標準エラー出力の転送)
package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	// SDK のパッケージ名も mcp なので、このパッケージと区別するため sdk と呼ぶ。
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// callTimeout は1回のリクエスト(接続時の問い合わせ、ツール一覧の取得、
// ツールの実行)の応答待ち上限。
//
// これがないと、応答を返さないサーバー1つでエージェント全体が固まる。
// ユーザーから見ると「Enterを押したらプロンプトが返ってこない」という
// 壊れ方になる。テストで短くできるよう、定数ではなく変数にしている。
var callTimeout = 30 * time.Second

// shutdownTimeout は終了時にサーバーの停止を待つ上限。
// これを過ぎると SDK が SIGTERM を送り、それでも終わらなければ強制終了する。
const shutdownTimeout = 3 * time.Second

// Server は接続するMCPサーバーの起動方法。
type Server struct {
	// Name はサーバーの識別名。ツール名の名前空間とログの接頭辞に使う。
	Name string
	// Command / Args は起動するコマンド。
	Command string
	Args    []string
	// Stderr はサーバーの標準エラー出力の転送先。nilなら捨てる。
	Stderr io.Writer
	// Log は送受信したJSON-RPCメッセージの書き出し先。nilなら書き出さない。
	// SDK に任せたやりとりを目で確かめたいときに使う。
	Log io.Writer
}

// Client は1つのMCPサーバーとの接続を表す。
type Client struct {
	serverName string
	session    *sdk.ClientSession
}

// Connect はMCPサーバーを子プロセスとして起動し、接続する。
func Connect(ctx context.Context, s Server) (*Client, error) {
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	// サーバーのログを捨てない。MCPサーバーが動かないときの手がかりは
	// たいてい標準エラー出力にしかなく、これを転送し忘れると
	// 「なぜか繋がらない」だけが残る。複数サーバーのログが混ざるので、
	// どのサーバーの出力かが分かるよう接頭辞を付ける。
	if s.Stderr != nil {
		cmd.Stderr = &prefixWriter{w: s.Stderr, prefix: "[mcp:" + s.Name + "] "}
	}

	// CommandTransport は子プロセスの起動と終了を受け持つ。Close では
	// 標準入力を閉じて終了を待ち(stdio transport の作法)、
	// TerminateDuration を過ぎても残っていれば SIGTERM、それでも
	// 終わらなければ強制終了する。
	var transport sdk.Transport = &sdk.CommandTransport{
		Command:           cmd,
		TerminateDuration: shutdownTimeout,
	}
	if s.Log != nil {
		transport = &sdk.LoggingTransport{Transport: transport, Writer: s.Log}
	}

	c, err := connect(ctx, s.Name, transport)
	if err != nil {
		return nil, fmt.Errorf("MCPサーバー %s に接続できませんでした: %w", s.Name, err)
	}
	return c, nil
}

// connect は transport を受け取ってサーバーに接続する。
// テストでは子プロセスの代わりに SDK の InMemoryTransport を渡す。
func connect(ctx context.Context, serverName string, t sdk.Transport) (*Client, error) {
	// 接続時の問い合わせ(server/discover)にも上限を置く。
	// 応答しないサーバーが1つあるだけで起動が止まらないようにするため。
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	client := sdk.NewClient(&sdk.Implementation{
		Name:    "go-coding-agent-handson",
		Version: "0.1.0",
	}, &sdk.ClientOptions{
		// サーバーからの問い合わせ(ユーザーへの質問など)には何も答えない
		// ので、capabilities は空で宣言する。指定しないと SDK は既定で
		// roots(2026-07-28 版で非推奨)を宣言する。
		Capabilities: &sdk.ClientCapabilities{},
	})
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, err
	}
	return &Client{serverName: serverName, session: session}, nil
}

// Close は接続を閉じ、子プロセスの終了を待つ。
func (c *Client) Close() error {
	return c.session.Close()
}

// prefixWriter は書き込みの各行の先頭に固定の文字列を足す io.Writer。
//
// 子プロセスの出力は行の途中で分割されて届くことがあるので、
// 改行が来るまで溜めてから書き出す。溜めずに Write ごとに接頭辞を
// 付けると、1行の途中に接頭辞が挟まる。
type prefixWriter struct {
	w      io.Writer
	prefix string
	buf    []byte
}

func (p *prefixWriter) Write(data []byte) (int, error) {
	p.buf = append(p.buf, data...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			return len(data), nil
		}
		line := append([]byte(p.prefix), p.buf[:i+1]...)
		p.buf = p.buf[i+1:]
		if _, err := p.w.Write(line); err != nil {
			return 0, err
		}
	}
}
