package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// serverFunc はテスト用の疑似サーバーの振る舞い。
// リクエスト1件を受け取り、返したいレスポンス(複数行可)を書き出す。
type serverFunc func(t *testing.T, req rawRequest, w io.Writer)

type rawRequest struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// startFakeServer はメモリ上で動く疑似MCPサーバーに繋いだクライアントを返す。
// 子プロセスの代わりに io.Pipe で接続し、プロトコルのやり取りを検証する。
func startFakeServer(t *testing.T, handle serverFunc) *Client {
	t.Helper()
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	go func() {
		defer serverWriter.Close()
		scanner := bufio.NewScanner(serverReader)
		for scanner.Scan() {
			var req rawRequest
			if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
				t.Errorf("サーバー: 不正なリクエスト: %v", err)
				return
			}
			if req.ID == nil {
				continue // notifications/initialized などの通知は応答不要
			}
			handle(t, req, serverWriter)
		}
	}()

	c := newClient("weather", clientWriter, clientReader)
	t.Cleanup(func() { _ = c.Close() })
	if err := c.initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

// writeResult は正常応答を1行書き出す。
func writeResult(t *testing.T, w io.Writer, id int, result any) {
	t.Helper()
	resp, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(append(resp, '\n'))
}

// weatherServer は get_weather ツールを1つ持つ標準的な疑似サーバー。
func weatherServer(t *testing.T, req rawRequest, w io.Writer) {
	switch req.Method {
	case "initialize":
		writeResult(t, w, *req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "fake", "version": "0.0.1"},
		})
	case "tools/list":
		writeResult(t, w, *req.ID, map[string]any{
			"tools": []map[string]any{{
				"name":        "get_weather",
				"description": "天気を返す",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
					"required":   []string{"city"},
				},
			}},
		})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		writeResult(t, w, *req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "晴れ (" + p.Name + ")"}},
		})
	}
}

func TestListAndCallTools(t *testing.T) {
	c := startFakeServer(t, weatherServer)
	ctx := t.Context()

	toolList, err := c.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(toolList) != 1 {
		t.Fatalf("ツールは1つのはず: %d", len(toolList))
	}

	tool := toolList[0]
	// サーバー名で名前空間が切られる。
	if got := tool.Name(); got != "mcp__weather__get_weather" {
		t.Errorf("ツール名: got %q", got)
	}
	// JSON SchemaがSDK用のSchemaに写されている。
	schema := tool.InputSchema()
	if _, ok := schema.Properties["city"]; !ok {
		t.Errorf("スキーマにcityがない: %+v", schema)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "city" {
		t.Errorf("requiredが写されていない: %+v", schema.Required)
	}

	// ツール実行 = tools/call の往復。
	got, err := tool.Run(ctx, json.RawMessage(`{"city":"Tokyo"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "晴れ (get_weather)" {
		t.Errorf("got %q", got)
	}
}

// サーバーは応答の前後に通知(IDなしのメッセージ)を勝手に送ってくる。
// これを応答と取り違えると、以降ずっと1つずれた応答を読むことになる。
func TestCallSkipsNotifications(t *testing.T) {
	c := startFakeServer(t, func(t *testing.T, req rawRequest, w io.Writer) {
		if req.Method == "initialize" {
			writeResult(t, w, *req.ID, map[string]any{"protocolVersion": protocolVersion})
			return
		}
		// 応答の前に通知を2件、別リクエストへの応答を1件混ぜる。
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}`+"\n")
		_, _ = io.WriteString(w, "\n") // 空行
		writeResult(t, w, 9999, map[string]any{"tools": []any{}})
		writeResult(t, w, *req.ID, map[string]any{
			"tools": []map[string]any{{"name": "real_tool", "description": "本物"}},
		})
	})

	infos, err := c.ListTools(t.Context())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "real_tool" {
		t.Errorf("通知や別IDの応答を取り違えている: %+v", infos)
	}
}

// サーバーがJSON-RPCのエラーオブジェクトを返した場合。
func TestCallReturnsRPCError(t *testing.T) {
	c := startFakeServer(t, func(t *testing.T, req rawRequest, w io.Writer) {
		if req.Method == "initialize" {
			writeResult(t, w, *req.ID, map[string]any{"protocolVersion": protocolVersion})
			return
		}
		resp, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": *req.ID,
			"error": map[string]any{"code": -32601, "message": "Method not found"},
		})
		_, _ = w.Write(append(resp, '\n'))
	})

	_, err := c.ListTools(t.Context())
	if err == nil {
		t.Fatal("エラー応答がエラーとして返っていない")
	}
	rpcErr, ok := errors.AsType[*rpcError](err)
	if !ok {
		t.Fatalf("rpcErrorとして返るべき: %T %v", err, err)
	}
	if rpcErr.Code != -32601 || !strings.Contains(rpcErr.Error(), "Method not found") {
		t.Errorf("エラーの内容が失われている: %v", rpcErr)
	}
}

// ツールがエラーを返した場合(JSON-RPCは成功、結果に isError が立つ)。
// プロトコル層のエラーとツール層のエラーは別物である。
func TestCallToolIsError(t *testing.T) {
	c := startFakeServer(t, func(t *testing.T, req rawRequest, w io.Writer) {
		if req.Method == "initialize" {
			writeResult(t, w, *req.ID, map[string]any{"protocolVersion": protocolVersion})
			return
		}
		writeResult(t, w, *req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "都市が見つかりません"}},
			"isError": true,
		})
	})

	_, err := c.CallTool(t.Context(), "get_weather", json.RawMessage(`{"city":"???"}`))
	if err == nil {
		t.Fatal("isError の結果がエラーとして返っていない")
	}
	if !strings.Contains(err.Error(), "都市が見つかりません") {
		t.Errorf("エラー本文が失われている: %v", err)
	}
}

// 応答を返さないサーバーで固まらないこと。
// ここが無期限待ちだと、サーバー1つでエージェント全体が沈黙する。
func TestCallTimesOutOnSilentServer(t *testing.T) {
	silent := func(t *testing.T, req rawRequest, w io.Writer) {
		if req.Method == "initialize" {
			writeResult(t, w, *req.ID, map[string]any{"protocolVersion": protocolVersion})
		}
		// initialize 以降は何も返さない。
	}
	c := startFakeServer(t, silent)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := c.ListTools(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("応答がないのに成功が返った")
		}
		if !strings.Contains(err.Error(), "応答しません") {
			t.Errorf("タイムアウトと分かるメッセージでない: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("応答しないサーバーで固まった")
	}
}

// タイムアウトで諦めたリクエストの応答が後から届いても、
// 次のリクエストが1つずれた応答を読まないこと。
func TestLateResponseDoesNotDesyncNextCall(t *testing.T) {
	c := startFakeServer(t, func(t *testing.T, req rawRequest, w io.Writer) {
		switch req.Method {
		case "initialize":
			writeResult(t, w, *req.ID, map[string]any{"protocolVersion": protocolVersion})
		case "tools/list":
			// 1回目は遅れて応答する(呼び出し側は先に諦めている)。
			time.Sleep(150 * time.Millisecond)
			writeResult(t, w, *req.ID, map[string]any{
				"tools": []map[string]any{{"name": "遅れて届いた応答"}},
			})
		case "tools/call":
			writeResult(t, w, *req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "正しい応答"}},
			})
		}
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.ListTools(ctx); err == nil {
		t.Fatal("1回目はタイムアウトするはず")
	}

	// 遅れた応答が届くのを待ってから次のリクエストを出す。
	time.Sleep(200 * time.Millisecond)
	got, err := c.CallTool(t.Context(), "get_weather", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("2回目が失敗した: %v", err)
	}
	if got != "正しい応答" {
		t.Errorf("応答がずれている: %q", got)
	}
}

// サーバーが落ちた(標準出力が閉じた)場合に、待ち続けずエラーを返すこと。
func TestCallFailsWhenServerExits(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()
	go func() {
		// initialize には応答するが、直後に標準出力を閉じて黙る。
		// 入力は読み続ける(io.Pipe は同期なので、読むのをやめると
		// クライアント側の書き込みが返ってこなくなる)。
		scanner := bufio.NewScanner(serverReader)
		first := true
		for scanner.Scan() {
			if !first {
				continue
			}
			first = false
			var req rawRequest
			_ = json.Unmarshal(scanner.Bytes(), &req)
			writeResult(t, serverWriter, *req.ID, map[string]any{"protocolVersion": protocolVersion})
			_ = serverWriter.Close()
		}
	}()
	t.Cleanup(func() { _ = clientWriter.Close() })

	c := newClient("dead", clientWriter, clientReader)
	if err := c.initialize(t.Context()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := c.ListTools(t.Context()); err == nil {
		t.Fatal("接続断がエラーになっていない")
	} else if !strings.Contains(err.Error(), "接続が切れました") {
		t.Errorf("接続断と分かるメッセージでない: %v", err)
	}
}

// ここから下は本物の子プロセスを相手にした検証。
// io.Pipe の疑似サーバーでは cmd が nil なので、Close() の後始末
// (プロセスの停止待ち・強制終了)がまったく通らない。
// 「エージェントを終了したのにプロンプトが返ってこない」は
// 実際に踏むと厄介なので、ここだけは実プロセスで確かめる。

// script は initialize に応答したあと after を実行するシェルスクリプト。
func script(after string) []string {
	return []string{"-c", `read -r line; printf '{"jsonrpc":"2.0","id":1,"result":{}}\n'; ` + after}
}

// 行儀のよいサーバー(stdinが閉じたら終了する)を正しく待って閉じること。
func TestCloseWaitsForServerExit(t *testing.T) {
	var stderr bytes.Buffer
	c, err := Connect(t.Context(), Server{
		Name: "polite",
		// 標準エラーに1行出してから、stdinのEOFを待って終了する。
		Command: "sh",
		Args:    script(`echo "起動しました" >&2; cat > /dev/null`),
		Stderr:  &stderr,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
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

	// サーバーのログが捨てられず、どのサーバーの出力か分かる形で届く。
	// (Close の中の Wait が標準エラーの転送完了まで待つので、
	//  ここで読んでも競合しない)
	if got := stderr.String(); got != "[mcp:polite] 起動しました\n" {
		t.Errorf("サーバーのログ: got %q", got)
	}
}

// stdinを閉じても終了しないサーバーを、待ち続けずに強制終了すること。
// この経路がないと、行儀の悪いサーバー1つでエージェントが終了できなくなる。
func TestCloseKillsUnresponsiveServer(t *testing.T) {
	c, err := Connect(t.Context(), Server{
		Name:    "stubborn",
		Command: "sh",
		// exec で置き換えるのは、シェルを kill しても子の sleep が
		// 生き残るのを避けるため。
		Args: script("exec sleep 60"),
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case <-done:
		// 強制終了なので Wait はエラー(signal: killed)を返す。内容は問わない。
	case <-time.After(shutdownTimeout + 10*time.Second):
		t.Fatal("終了しないサーバーで Close が固まった")
	}
	if elapsed := time.Since(start); elapsed < shutdownTimeout {
		t.Errorf("待たずに強制終了している: %v", elapsed)
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
