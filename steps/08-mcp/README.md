# step 08: MCPクライアント — 外部プロセスのツールをつなぐ

ここまでのツール(read_file、bash、task……)はすべて、エージェントと一緒にコンパイルされる組み込みツールでした。

MCP(Model Context Protocol)は、ツールを**外部プロセス(MCPサーバー)**として提供するための標準プロトコルです。ツールの名前・説明・入力スキーマの取得と実行のしかたが決まっているので、MCPに対応したエージェントなら、同じサーバーをそのまま使えます。Claude Code 向けに書かれたMCPサーバーを、このミニエージェントにもつなげられるということです。

このstepでは、公式Go SDK([modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk))を使って、MCPの stdio transport クライアントを実装します。

## この章で学ぶこと

- stdio transport の仕組み(子プロセス + 改行区切りJSON-RPC 2.0)と、server/discover → tools/list → tools/call の流れ
- SDK に任せること(JSON-RPCの読み書き、id の突き合わせ、バージョンの交渉)と、自分で書くこと
- MCPのツールを `tools.Tool` インターフェースにラップして、エージェントループにそのまま渡す
- 外部プロセスに対する最低限の備え(応答待ちの上限、終了しないサーバー、標準エラー出力)
- 設定ファイルは実行権限そのものである、という事実(起動時の確認)

## 前のstepから引き継いだもの

step 07の解答(Task tool)は埋まった状態で始まります。今回増えたのは:

- [mcp/client.go](mcp/client.go) — サーバーの起動と接続、終了(実装済み)
- [mcp/tool.go](mcp/tool.go) — MCPのツールを `tools.Tool` にラップするアダプタ(課題)
- [config/config.go](config/config.go) — `.mcp.json` の読み込みと `Executables`(実装済み)
- [main.go](main.go) — サーバーへの接続と、起動時に設定ファイルのサーバーを起動してよいかの確認(実装済み)

## SDK がやってくれること / 自分で書くこと

stdio transport の仕組みは素朴です。サーバーを子プロセスとして起動し、標準入出力で1行1メッセージのJSON-RPC 2.0を交換します。使うメソッドは3つで、`server/discover`(サーバーが対応するバージョンの問い合わせ)→ `tools/list`(ツール一覧の取得)→ `tools/call`(実行)です。

素朴でも、手で書くと考えることは多くあります。ブロックする読み取りに期限を付けるためのゴルーチン、サーバーが勝手に送ってくる通知や、タイムアウトで諦めたリクエストへの遅れたレスポンスを読み飛ばすための id の突き合わせ、サーバーと共通のプロトコルのバージョンを決める交渉です。2026-07-28 版でMCPはステートレスになり、`initialize` のハンドシェイクの代わりに、どのリクエストも `_meta` に自分のバージョンと capabilities を入れて送るようになりました。SDK は新しい方式で問い合わせ、古いサーバーには `initialize` で接続し直します。

このstepでは、これらを SDK に任せます。自分で書くのは次の2つです。

1. **ラップ**: SDK が返すツール(`*sdk.Tool`)を `tools.Tool` インターフェースに合わせる。ツール名は `mcp__サーバー名__ツール名` にして、複数のサーバーが同名のツールを持っていても衝突しないようにします。エージェント本体から見れば組み込みツールもMCPツールも区別がありません
2. **備え**: 相手は別のプロセスなので、応答しないことがあります。応答待ちに上限(`callTimeout`)を置かないと、Enterを押したらプロンプトが返ってこない、という壊れ方をします。終了しないサーバーは SDK の `CommandTransport` が SIGTERM、強制終了の順で止め、標準エラー出力は `[mcp:サーバー名]` を付けて転送します(どちらも実装済み)

SDK が送受信しているJSONを見たいときは、環境変数 `AGENT_MCP_LOG=1` を付けて起動してください。`sdk.LoggingTransport` が全メッセージを標準エラー出力に書き出します。

## 設定ファイルは実行権限そのものである

`.mcp.json` に書かれた `command` は、エージェントの**起動と同時に子プロセスとして実行**されます。つまり悪意あるリポジトリは、`.mcp.json` に1行書いておくだけで、cloneした人がエージェントを起動した瞬間に任意のコマンドを実行させられます。

そこで [main.go](main.go) の `confirmTrust` は、起動時に「このディレクトリの設定は何を実行しようとしているか」を一覧表示し、起動してよいかを1度だけ確認します。一覧には bash の allowlist も含めます。「確認なしで実行してよい」の宣言は、承認フローを最初から一部素通りにするものだからです。拒否されたら起動を中止します。確認が取れない(標準入力が繋がっていない)場合も中止です。「聞けなかったから通す」は安全機構としてありえません。

## 課題

**TODO(step08-1)** [mcp/tool.go](mcp/tool.go) — `Client.Tools`。サーバーのツール一覧を取得し、1つずつ `*Tool` にラップして返します。

**TODO(step08-2)** [mcp/tool.go](mcp/tool.go) — `Tool.Run`。応答待ちに上限を置いて `tools/call` を呼び、結果のテキストを返します。ツールの失敗(`isError`)はエラーにします。

## 検証

```bash
make check STEP=08
```

テストは SDK のサーバー機能でテスト用のMCPサーバーを立て、メモリ上の接続(`sdk.NewInMemoryTransports`)でつなぎます。`TestToolsWrapsServerTools`(ラップ)、`TestRunCallsServerTool` と `TestRunReturnsToolError`(呼び出しと失敗)、`TestRunTimesOutOnSlowTool`(応答しないツールを上限で諦め、遅れて届いた結果で次の呼び出しがずれないこと)、`TestRunStopsOnCancel`(Ctrl-C)がこのstepの課題に対応します。子プロセスの起動・終了は、テストのバイナリ自身をサーバーとして起動して確かめています。

## 動かしてみよう(任意)

```bash
export ANTHROPIC_API_KEY=sk-ant-...
cd example
go run ../steps/08-mcp
```

起動すると、設定ファイルのサーバーを起動してよいかの確認が表示されます。`example/.mcp.json` のサーバー定義は空ですが、`.agent/settings.json` の allowlist が「承認なしで実行を許可」の宣言として一覧に載るためです。`n` と答えると起動が中止されることも確かめてください。

手元にMCPサーバーがあれば(npxで動く公開サーバーでも、SDK で書いた数十行のサーバーでも)、`.mcp.json` に `{"mcpServers": {"名前": {"command": "...", "args": [...]}}}` と書くだけで、そのツールがエージェントに加わります。確認の一覧にそのコマンドが出ることと、`AGENT_MCP_LOG=1` でやりとりのJSONが見えることも確かめてください。

## 次のstepへ

ツールは揃いました。step 09では、決まった手順や専門知識をエージェントに渡す仕組み——ユーザーが呼ぶスラッシュコマンドと、LLMが自分で読むスキル——を実装します。同じMarkdownの手順書でも、**呼ぶ主体**が違うと設計がどう変わるかが見どころです。
