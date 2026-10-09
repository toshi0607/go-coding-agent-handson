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
//   - server/discover: サーバーが対応するバージョンと能力を問い合わせる
//   - tools/list:      サーバーが提供するツールの一覧を取得
//   - tools/call:      ツールを実行
//
// 接続に状態はない。「最初に名乗って以後はそれを前提にする」のではなく、
// どのリクエストも自分のプロトコルバージョンを _meta に入れて送り、
// サーバーは1件ずつ独立に受け付けるか拒否するかを決める。
//
// 素朴なぶん、相手が「他人の書いたプロセス」であることの難しさは
// こちらが引き受けることになる。応答が返ってこない、終了させても
// 死なない、エラーを標準エラー出力にだけ吐く——外部プロセスと
// 話す以上どれも普通に起きるので、そのための備えがこのファイルの
// 分量の多くを占めている。
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// protocolVersion はこのクライアントが話すMCPのプロトコルバージョン。
//
// 2026-07-28 版でMCPはステートレスになった。それ以前の版にあった
// initialize ハンドシェイクは廃止され、バージョンとクライアントの能力は
// リクエストごとに _meta で送る。このクライアントが話すのはこの方式だけで、
// ハンドシェイク方式しか話せない古いサーバーには接続できない。
const protocolVersion = "2026-07-28"

// errUnsupportedProtocolVersion は、サーバーが要求されたバージョンに
// 対応していないことを示すJSON-RPCエラーコード。
const errUnsupportedProtocolVersion = -32022

const (
	// callTimeout は1回のリクエストの応答待ち上限。
	// これがないと、応答を返さないサーバー1つでエージェント全体が
	// 無言で固まる。ユーザーから見ると「Enterを押したら二度と
	// プロンプトが返ってこない」という最悪の壊れ方になる。
	callTimeout = 30 * time.Second

	// shutdownTimeout は終了時にサーバーの停止を待つ上限。
	shutdownTimeout = 3 * time.Second

	// maxMessageBytes は1メッセージの最大サイズ(大きなツール結果に備える)。
	maxMessageBytes = 10 * 1024 * 1024

	// lineBufferSize は読み取りゴルーチンの先読みバッファ段数。
	// サーバーが立て続けに書いても、こちらが読むまで子プロセスが
	// ブロックしないようにするための余裕。
	lineBufferSize = 16
)

// Server は接続するMCPサーバーの起動方法。
type Server struct {
	// Name はサーバーの識別名。ツール名の名前空間とログの接頭辞に使う。
	Name string
	// Command / Args は起動するコマンド。
	Command string
	Args    []string
	// Stderr はサーバーの標準エラー出力の転送先。nilなら捨てる。
	Stderr io.Writer
}

// message は読み取りゴルーチンから届く1件のメッセージ、
// または読み取りの終了理由。
type message struct {
	line []byte
	err  error
}

// Client は1つのMCPサーバーとの接続を表す。
type Client struct {
	serverName string
	writer     io.Writer
	nextID     int
	cmd        *exec.Cmd // 子プロセス起動時のみ非nil

	// lines は読み取りゴルーチンからの受け口。
	//
	// なぜ直接 Read せずゴルーチンを挟むのか。ブロックしている
	// Read は context のキャンセルでは中断できないからである。
	// 「別のゴルーチンで読み、こちらは channel と ctx.Done() を
	// select する」のが、GoでブロッキングI/Oに期限を付ける定石。
	lines chan message
	// closed は読み取りゴルーチンを諦めさせるための合図。
	closed chan struct{}
	// readDone は読み取りゴルーチンが終了したことを示す。
	readDone chan struct{}
}

// Connect はMCPサーバーを子プロセスとして起動し、話が通じる相手かを確かめる。
func Connect(ctx context.Context, s Server) (*Client, error) {
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// サーバーのログを捨てない。MCPサーバーが動かないときの手がかりは
	// たいてい標準エラー出力にしかなく、これを配線し忘れると
	// 「なぜか繋がらない」だけが残る。複数サーバーのログが混ざるので、
	// どのサーバーの出力かが分かるよう接頭辞を付ける。
	if s.Stderr != nil {
		cmd.Stderr = &prefixWriter{w: s.Stderr, prefix: "[mcp:" + s.Name + "] "}
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("MCPサーバー %s の起動に失敗: %w", s.Name, err)
	}

	c := newClient(s.Name, stdin, stdout)
	c.cmd = cmd
	if err := c.discover(ctx); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("MCPサーバー %s に接続できません: %w", s.Name, err)
	}
	return c, nil
}

// newClient は任意の入出力ペアからクライアントを作る。
// テストでは子プロセスの代わりに io.Pipe を渡して疑似サーバーと会話する。
func newClient(serverName string, w io.Writer, r io.Reader) *Client {
	c := &Client{
		serverName: serverName,
		writer:     w,
		lines:      make(chan message, lineBufferSize),
		closed:     make(chan struct{}),
		readDone:   make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

// readLoop はサーバーの出力を1行ずつ読んで channel に流す。
func (c *Client) readLoop(r io.Reader) {
	defer close(c.readDone)
	defer close(c.lines)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	for scanner.Scan() {
		// scanner.Bytes() の返すスライスは次の Scan で上書きされるので複製する。
		select {
		case c.lines <- message{line: bytes.Clone(scanner.Bytes())}:
		case <-c.closed:
			return // Close() が諦めた。送信待ちのまま残らないよう抜ける
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case c.lines <- message{err: err}:
		case <-c.closed:
		}
	}
}

// Close は接続を閉じ、子プロセスの終了を待つ。
func (c *Client) Close() error {
	// stdin を閉じるとサーバーは終了する(stdio transport の作法)。
	if closer, ok := c.writer.(io.Closer); ok {
		_ = closer.Close()
	}
	if c.cmd == nil {
		return nil
	}

	// 読み取りゴルーチンの終了 = stdout のEOF = サーバーの終了。
	// 終わらないサーバーを待ち続けるとエージェント自体が終了できなく
	// なるので、上限を切って強制終了に切り替える。
	select {
	case <-c.readDone:
	case <-time.After(shutdownTimeout):
		_ = c.cmd.Process.Kill()
		close(c.closed) // 送信待ちで止まっているゴルーチンを解放する
		<-c.readDone
	}
	// Wait は「stdoutからの読み取りが全部終わってから」呼ぶ必要がある。
	// 上で readDone を待っているのはそのためでもある。
	return c.cmd.Wait()
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
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("MCPエラー %d: %s", e.Code, e.Message)
}

// call はリクエストを送り、対応するレスポンスを待つ。
//
// 待ち時間には必ず上限を置く。ここで無期限に待つと、応答しない
// サーバーが1つあるだけでエージェントが固まり、Ctrl-Cしか
// 残らなくなる。ctx は呼び出し側のキャンセル(Ctrl-C)も運んでくる。
func (c *Client) call(ctx context.Context, method string, params map[string]any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	params = maps.Clone(params)
	if params == nil {
		params = make(map[string]any)
	}
	params["_meta"] = requestMeta()

	c.nextID++
	id := c.nextID
	if err := c.send(request{Jsonrpc: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		return err
	}

	// TODO(step08-1): 自分のリクエスト(id)に対する応答が来るまで
	// c.lines を読み続けるループを実装する。満たすべき仕様:
	//
	//  - ctx.Done() と c.lines の select で待つ。ctx が切れたら
	//    「どのサーバーの何が応答しないか」が分かるエラーを返す。
	//    ここが無期限待ちだと、応答しないサーバー1つでエージェント全体が
	//    無言で固まる(ユーザーにはCtrl-Cしか残らない)
	//  - c.lines が閉じられていたら(受信の ok が false)、接続が
	//    切れたことが分かるエラーを返す
	//  - msg.err が入っていたらそれを返す。空行は読み飛ばす
	//  - 1行を response としてパースし、次のものは読み飛ばして待ち続ける:
	//      - ID の無いメッセージ(サーバーが勝手に送ってくる通知)
	//      - 自分の id と違う応答(タイムアウトで諦めた過去のリクエストへの
	//        応答が、後から届くことがある)
	//    ID を見ずに「次に来た1行」を答えとみなすと、一度タイムアウト
	//    しただけで以降ずっと1つずつずれた応答を読み続けることになる
	//  - resp.Error が入っていたらそれを返す
	//  - 自分宛ての正常応答なら、result が非nilのとき resp.Result を
	//    json.Unmarshal してから nil を返す
	panic("TODO(step08-1): steps/08-mcp/mcp/client.go を実装してください(hints.md 参照)")
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

// requestMeta は全リクエストの _meta に入れる、プロトコル上の必須情報を返す。
//
// ハンドシェイクがないので、サーバーはこれを見て初めて「相手がどの版を
// 話していて、何ができるクライアントなのか」を知る。能力(capabilities)が
// 空なのは、このクライアントがサーバーからの追加入力の要求などに
// 一切対応しないことの宣言である。
func requestMeta() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion":    protocolVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"io.modelcontextprotocol/clientInfo": map[string]any{
			"name":    "go-coding-agent-handson",
			"version": "0.1.0",
		},
	}
}

// discover は server/discover でサーバーが対応するバージョンを問い合わせ、
// このクライアントと話が通じる相手かを確かめる。
//
// 仕様上、この問い合わせは省略できる。いきなり tools/list を送り、
// バージョンが合わなければエラーで知る、でも動く。それでも最初に
// 送るのは、ハンドシェイク方式の古いサーバーを確実に見分けるためである。
// 古いサーバーの中には initialize を経ていないリクエストでも
// 処理してしまうものがあり、tools/call は新旧どちらの版にもある。
// 確かめずに進むと、古い流儀のまま半端に動いてしまう。
func (c *Client) discover(ctx context.Context) error {
	var result struct {
		ResultType        string   `json:"resultType"`
		SupportedVersions []string `json:"supportedVersions"`
	}
	if err := c.call(ctx, "server/discover", nil, &result); err != nil {
		if rpcErr, ok := errors.AsType[*rpcError](err); ok && rpcErr.Code == errUnsupportedProtocolVersion {
			var data struct {
				Supported []string `json:"supported"`
			}
			if err := json.Unmarshal(rpcErr.Data, &data); err != nil {
				return fmt.Errorf("サーバーが対応する版のパースに失敗: %w", err)
			}
			return unsupportedProtocolVersion(data.Supported)
		}
		return fmt.Errorf("server/discover に失敗しました。MCP %s より前の、initialize ハンドシェイク方式のサーバーかもしれません: %w", protocolVersion, err)
	}
	if err := checkResultType(result.ResultType); err != nil {
		return err
	}
	if !slices.Contains(result.SupportedVersions, protocolVersion) {
		return unsupportedProtocolVersion(result.SupportedVersions)
	}
	return nil
}

func unsupportedProtocolVersion(supported []string) error {
	return fmt.Errorf("サーバーはMCP %s に対応していません(サーバーが対応する版: %s)", protocolVersion, strings.Join(supported, ", "))
}

// checkResultType は結果の種別を確かめる。
//
// 2026-07-28 版の結果には必ず resultType が付く。"complete" が通常の
// 完了で、"input_required" は「続けるには追加の入力が要る」という
// サーバーからの差し戻しである。このクライアントは能力を何も宣言して
// いないので差し戻しは来ないはずだが、来たら黙って空の結果として
// 扱わず、エラーにする。フィールドが無い場合は完了とみなす
// (古い版のサーバーとの互換のため、仕様がそう定めている)。
func checkResultType(resultType string) error {
	switch resultType {
	case "", "complete":
		return nil
	case "input_required":
		return errors.New("サーバーが追加の入力を要求しましたが、このクライアントは対応していません")
	default:
		return fmt.Errorf("不明な resultType: %q", resultType)
	}
}

// ToolInfo はサーバーが提供するツールのメタデータ。
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ListTools はサーバーが提供するツールの一覧を取得する。
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	var result struct {
		ResultType string     `json:"resultType"`
		Tools      []ToolInfo `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	if err := checkResultType(result.ResultType); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool はツールを実行し、テキスト結果を返す。
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}
	var result struct {
		ResultType string `json:"resultType"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return "", err
	}

	if err := checkResultType(result.ResultType); err != nil {
		return "", err
	}

	var text strings.Builder
	for _, content := range result.Content {
		if content.Type == "text" {
			text.WriteString(content.Text)
		}
	}
	if result.IsError {
		return "", fmt.Errorf("MCPツール %s がエラーを返しました: %s", name, text.String())
	}
	return text.String(), nil
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
