// Package mcp はMCP(Model Context Protocol)のクライアントを実装する。
//
// MCPはツールを外部プロセスとして提供するための標準プロトコルである。
// これまでのツール(read_fileなど)はエージェントにコンパイルされて
// いたが、MCPならエージェントを再ビルドせずに、他人が作ったツールを
// 設定ファイル1行で追加できる。ツールのプラグイン機構と言ってよい。
//
// stdio transport の仕組みは素朴で、
//   - サーバーを子プロセスとして起動する
//   - 標準入出力を通じて、改行区切りのJSON-RPC 2.0メッセージを交換する
//
// だけである。使うメソッドは3つ:
//   - initialize:  ハンドシェイク(バージョンと能力のすり合わせ)
//   - tools/list:  サーバーが提供するツールの一覧を取得
//   - tools/call:  ツールを実行
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

// protocolVersion はこのクライアントが話すMCPのプロトコルバージョン。
const protocolVersion = "2025-06-18"

// Client は1つのMCPサーバーとの接続を表す。
type Client struct {
	serverName string
	writer     io.Writer
	scanner    *bufio.Scanner
	nextID     int
	cmd        *exec.Cmd // 子プロセス起動時のみ非nil
}

// Connect はMCPサーバーを子プロセスとして起動し、ハンドシェイクまで済ませる。
func Connect(ctx context.Context, serverName, command string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("MCPサーバー %s の起動に失敗: %w", serverName, err)
	}

	c := newClient(serverName, stdin, stdout)
	c.cmd = cmd
	if err := c.initialize(); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("MCPサーバー %s の初期化に失敗: %w", serverName, err)
	}
	return c, nil
}

// newClient は任意の入出力ペアからクライアントを作る。
// テストでは子プロセスの代わりに io.Pipe を渡して疑似サーバーと会話する。
func newClient(serverName string, w io.Writer, r io.Reader) *Client {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // 大きなツール結果に備える
	return &Client{serverName: serverName, writer: w, scanner: scanner}
}

// Close は接続を閉じ、子プロセスの終了を待つ。
func (c *Client) Close() error {
	if closer, ok := c.writer.(io.Closer); ok {
		_ = closer.Close() // stdinを閉じるとサーバーは終了する
	}
	if c.cmd != nil {
		return c.cmd.Wait()
	}
	return nil
}

// ---- JSON-RPC 2.0 のメッセージ型 ----

type request struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"` // 通知(notification)ではIDを省く
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("MCPエラー %d: %s", e.Code, e.Message)
}

// call はリクエストを送り、対応するレスポンスを待つ。
func (c *Client) call(method string, params any, result any) error {
	c.nextID++
	id := c.nextID
	if err := c.send(request{Jsonrpc: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		return err
	}

	// 自分のリクエストIDに対する応答が来るまで読む。
	// サーバーは通知(IDなしのメッセージ)を勝手に送ってくることが
	// あるので、それらは読み飛ばす。
	for c.scanner.Scan() {
		line := c.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp response
		if err := json.Unmarshal(line, &resp); err != nil {
			return fmt.Errorf("MCPレスポンスのパースに失敗: %w", err)
		}
		if resp.ID == nil || *resp.ID != id {
			continue // 通知や別リクエストへの応答
		}
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	}
	if err := c.scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("MCPサーバー %s との接続が切れました", c.serverName)
}

// send はメッセージを1行のJSONとして書き込む(改行区切りフレーミング)。
func (c *Client) send(req request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.writer.Write(append(data, '\n'))
	return err
}

// initialize はMCPのハンドシェイクを行う。
// initialize リクエスト → 応答 → initialized 通知、の3ステップ。
func (c *Client) initialize() error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "go-coding-agent-handson",
			"version": "0.1.0",
		},
	}
	if err := c.call("initialize", params, nil); err != nil {
		return err
	}
	// initialized は通知なのでIDを付けず、応答も待たない。
	return c.send(request{Jsonrpc: "2.0", Method: "notifications/initialized"})
}

// ToolInfo はサーバーが提供するツールのメタデータ。
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ListTools はサーバーが提供するツールの一覧を取得する。
func (c *Client) ListTools() ([]ToolInfo, error) {
	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := c.call("tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool はツールを実行し、テキスト結果を返す。
func (c *Client) CallTool(name string, arguments json.RawMessage) (string, error) {
	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call("tools/call", params, &result); err != nil {
		return "", err
	}

	var text string
	for _, content := range result.Content {
		if content.Type == "text" {
			text += content.Text
		}
	}
	if result.IsError {
		return "", fmt.Errorf("MCPツール %s がエラーを返しました: %s", name, text)
	}
	return text, nil
}
