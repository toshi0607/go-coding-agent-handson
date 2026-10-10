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
	c := newFakeClient(t, handle)
	if err := c.discover(t.Context()); err != nil {
		t.Fatalf("discover: %v", err)
	}
	return c
}

// newFakeClient は接続時の問い合わせをせず、失敗するサーバーも検証できるようにする。
func newFakeClient(t *testing.T, handle serverFunc) *Client {
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
				continue // 通知は応答不要
			}
			handle(t, req, serverWriter)
		}
	}()

	c := newClient("weather", clientWriter, clientReader)
	t.Cleanup(func() { _ = c.Close() })
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

// writeDiscover はこのクライアントの版に対応するサーバーの正常応答を書く。
func writeDiscover(t *testing.T, w io.Writer, id int) {
	t.Helper()
	writeResult(t, w, id, map[string]any{
		"resultType":        "complete",
		"supportedVersions": []string{protocolVersion},
		"capabilities":      map[string]any{"tools": map[string]any{}},
	})
}

// writeError はJSON-RPCエラーと、その理由を示すデータを書く。
func writeError(t *testing.T, w io.Writer, id, code int, message string, data any) {
	t.Helper()
	resp, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message, "data": data},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(append(resp, '\n'))
}

// weatherServer は get_weather ツールを1つ持つ標準的な疑似サーバー。
func weatherServer(t *testing.T, req rawRequest, w io.Writer) {
	switch req.Method {
	case "server/discover":
		writeDiscover(t, w, *req.ID)
	case "tools/list":
		writeResult(t, w, *req.ID, map[string]any{
			"resultType": "complete",
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
			"resultType": "complete",
			"content":    []map[string]any{{"type": "text", "text": "晴れ (" + p.Name + ")"}},
		})
	}
}

// 各リクエストに版と能力がなければ、サーバーは独立に受け付ける判断ができない。
func TestRequestsCarryProtocolMeta(t *testing.T) {
	c := startFakeServer(t, func(t *testing.T, req rawRequest, w io.Writer) {
		var params map[string]json.RawMessage
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Errorf("params: %v", err)
		}
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(params["_meta"], &meta); err != nil {
			t.Errorf("_meta: %v", err)
		}
		var version string
		if err := json.Unmarshal(meta["io.modelcontextprotocol/protocolVersion"], &version); err != nil {
			t.Errorf("protocolVersion: %v", err)
		}
		if version != protocolVersion {
			t.Errorf("%s の版: got %q, want %q", req.Method, version, protocolVersion)
		}
		var capabilities map[string]any
		if err := json.Unmarshal(meta["io.modelcontextprotocol/clientCapabilities"], &capabilities); err != nil {
			t.Errorf("clientCapabilities: %v", err)
		}
		if capabilities == nil || len(capabilities) != 0 {
			t.Errorf("能力は空のオブジェクトのはず: %v", capabilities)
		}
		var info struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal(meta["io.modelcontextprotocol/clientInfo"], &info); err != nil {
			t.Errorf("clientInfo: %v", err)
		}
		if info.Name != "go-coding-agent-handson" || info.Version != "0.1.0" {
			t.Errorf("クライアント情報: %+v", info)
		}
		if req.Method == "server/discover" || req.Method == "tools/list" {
			if len(params) != 1 {
				t.Errorf("%s のparamsは_metaだけのはず: %s", req.Method, req.Params)
			}
		}
		if req.Method == "tools/call" {
			if string(params["name"]) != `"get_weather"` || string(params["arguments"]) != `{"city":"Tokyo"}` {
				t.Errorf("ツール名や引数が失われている: %s", req.Params)
			}
		}
		weatherServer(t, req, w)
	})
	if _, err := c.ListTools(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CallTool(t.Context(), "get_weather", json.RawMessage(`{"city":"Tokyo"}`)); err != nil {
		t.Fatal(err)
	}
}

// 古いサーバーを接続時に見分けないと、新旧の流儀が混ざったまま動いてしまう。
func TestDiscoverRejectsLegacyServer(t *testing.T) {
	c := newFakeClient(t, func(t *testing.T, req rawRequest, w io.Writer) {
		if req.Method != "server/discover" {
			t.Errorf("最初のメソッド: %q", req.Method)
		}
		writeError(t, w, *req.ID, -32601, "Method not found", nil)
	})
	err := c.discover(t.Context())
	if err == nil || !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("古い方式と分かるエラーが必要: %v", err)
	}
}

// 非対応の版を報告しないと、利用者は接続先が話せる版を判断できない。
func TestDiscoverReportsUnsupportedVersion(t *testing.T) {
	for _, responseType := range []string{"error", "result"} {
		t.Run(responseType, func(t *testing.T) {
			c := newFakeClient(t, func(t *testing.T, req rawRequest, w io.Writer) {
				if responseType == "error" {
					writeError(t, w, *req.ID, -32022, "Unsupported protocol version", map[string]any{
						"supported": []string{"2027-01-01"}, "requested": protocolVersion,
					})
					return
				}
				writeResult(t, w, *req.ID, map[string]any{
					"resultType": "complete", "supportedVersions": []string{"2027-01-01"},
				})
			})
			err := c.discover(t.Context())
			if err == nil || !strings.Contains(err.Error(), "2027-01-01") || !strings.Contains(err.Error(), protocolVersion) {
				t.Fatalf("要求した版と対応する版を含むエラーが必要: %v", err)
			}
		})
	}
}

// 追加入力の要求を空の成功結果と扱うと、ツールが完了したように見えてしまう。
func TestInputRequiredResultIsError(t *testing.T) {
	c := startFakeServer(t, func(t *testing.T, req rawRequest, w io.Writer) {
		if req.Method == "server/discover" {
			writeDiscover(t, w, *req.ID)
			return
		}
		writeResult(t, w, *req.ID, map[string]any{"resultType": "input_required", "content": []any{}})
	})
	if _, err := c.CallTool(t.Context(), "get_weather", json.RawMessage(`{}`)); err == nil {
		t.Fatal("追加入力の要求がエラーになっていない")
	}
}

// 結果の種別が省略された応答を拒否すると、仕様上有効な完了結果も使えない。
func TestMissingResultTypeMeansComplete(t *testing.T) {
	c := startFakeServer(t, func(t *testing.T, req rawRequest, w io.Writer) {
		switch req.Method {
		case "server/discover":
			writeDiscover(t, w, *req.ID)
		case "tools/list":
			writeResult(t, w, *req.ID, map[string]any{"tools": []map[string]any{{"name": "get_weather"}}})
		case "tools/call":
			writeResult(t, w, *req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "晴れ"}}})
		}
	})
	infos, err := c.ListTools(t.Context())
	if err != nil || len(infos) != 1 || infos[0].Name != "get_weather" {
		t.Fatalf("省略された種別の一覧結果: %+v, %v", infos, err)
	}
	got, err := c.CallTool(t.Context(), "get_weather", json.RawMessage(`{}`))
	if err != nil || got != "晴れ" {
		t.Fatalf("省略された種別の実行結果: %q, %v", got, err)
	}
}

// 未知の種別を受け入れると、解釈できない結果が成功として返ってしまう。
func TestInvalidResultTypeIsError(t *testing.T) {
	for _, method := range []string{"server/discover", "tools/list", "tools/call"} {
		t.Run(method, func(t *testing.T) {
			c := newFakeClient(t, func(t *testing.T, req rawRequest, w io.Writer) {
				writeResult(t, w, *req.ID, map[string]any{
					"resultType": "unknown", "supportedVersions": []string{protocolVersion},
				})
			})
			var err error
			switch method {
			case "server/discover":
				err = c.discover(t.Context())
			case "tools/list":
				_, err = c.ListTools(t.Context())
			case "tools/call":
				_, err = c.CallTool(t.Context(), "get_weather", json.RawMessage(`{}`))
			}
			if err == nil || !strings.Contains(err.Error(), `不明な resultType: "unknown"`) {
				t.Fatalf("未知の種別を拒否するエラーが必要: %v", err)
			}
		})
	}
}

// _metaの付与で元の引数を書き換えると、呼び出し側が再利用するデータも変わる。
func TestCallPreservesParams(t *testing.T) {
	c := startFakeServer(t, weatherServer)
	for _, hasMeta := range []bool{false, true} {
		params := map[string]any{"name": "get_weather", "arguments": json.RawMessage(`{}`)}
		if hasMeta {
			params["_meta"] = map[string]any{"caller": "keep"}
		}
		before, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.call(t.Context(), "tools/call", params, nil); err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Errorf("呼び出し側のparamsが変わった: before %s, after %s", before, after)
		}
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
		if req.Method == "server/discover" {
			writeDiscover(t, w, *req.ID)
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
		if req.Method == "server/discover" {
			writeDiscover(t, w, *req.ID)
			return
		}
		writeError(t, w, *req.ID, -32601, "Method not found", nil)
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
		if req.Method == "server/discover" {
			writeDiscover(t, w, *req.ID)
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
		if req.Method == "server/discover" {
			writeDiscover(t, w, *req.ID)
		}
		// server/discover 以降は何も返さない。
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
		case "server/discover":
			writeDiscover(t, w, *req.ID)
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
		// server/discover には応答するが、直後に標準出力を閉じて黙る。
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
			writeDiscover(t, serverWriter, *req.ID)
			_ = serverWriter.Close()
		}
	}()
	t.Cleanup(func() { _ = clientWriter.Close() })

	c := newClient("dead", clientWriter, clientReader)
	if err := c.discover(t.Context()); err != nil {
		t.Fatalf("discover: %v", err)
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

// script は server/discover に応答したあと after を実行するシェルスクリプト。
func script(after string) []string {
	return []string{"-c", `read -r line; printf '{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete","supportedVersions":["` + protocolVersion + `"],"capabilities":{"tools":{}}}}\n'; ` + after}
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
