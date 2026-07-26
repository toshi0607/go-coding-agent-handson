# go-coding-agent-handson

Zenn本「ミニClaude CodeをGoで実装して学ぶ コーディングエージェントのしくみ」の教材リポジトリです。

約2,000行のGoコードで、コーディングエージェントの中核機能をひととおり備えたミニエージェントを実装します。エージェントの正体は「LLM呼び出し → ツール実行 → 結果を返して再呼び出し」のループであり、このリポジトリはそのループを隠さず全部見せることを目的にしています。

## 構成

```
final/          完成形のミニコーディングエージェント
├── main.go       部品の組み立てとREPL
├── agent/        エージェントループ本体 + サブエージェント(Task tool)
├── llm/          LLM呼び出しの境界(実API / 録画リプレイ)
├── tools/        read_file, list_files, edit_file, bash
├── permission/   承認フロー(allowlist)
├── prompt/       システムプロンプト + CLAUDE.md読み込み
├── contextmgr/   トークン管理・compaction・ツール結果の切り詰め
├── mcp/          MCPクライアント(stdio transport)
├── command/      スラッシュコマンド
├── hooks/        PreToolUse / PostToolUse フック
└── config/       設定ファイルの読み込み
example/        エージェントを動かして遊ぶためのサンプル作業ディレクトリ
```

`steps/`(穴埋め式の段階実装)は本の公開に合わせて追加予定です。

## 動かし方

Go 1.24以降と、AnthropicのAPIキーが必要です。

```bash
export ANTHROPIC_API_KEY=sk-ant-...

cd example
go run ../final
```

```
> このディレクトリに何がある?
> hello/main.go を読んで、バグがあれば直して
> /help
```

ワンショット実行もできます:

```bash
go run ../final -p "hello/main.go のバグを直して"
```

モデルは環境変数で変更できます(デフォルト: claude-opus-5):

```bash
export ANTHROPIC_MODEL=claude-sonnet-5
```

## 作業ディレクトリの設定ファイル

エージェントはカレントディレクトリの次のファイルを読みます(すべて任意)。

| ファイル | 役割 |
|---|---|
| `CLAUDE.md` | プロジェクトメモリ。システムプロンプトに注入される |
| `.agent/settings.json` | bashコマンドのallowlistとhooks定義 |
| `.agent/commands/<名前>.md` | カスタムスラッシュコマンド(`$ARGUMENTS`が引数に展開される) |
| `.mcp.json` | 接続するMCPサーバーの定義 |

書き方は `example/` の実物を参照してください。

## テスト

LLM呼び出しはインターフェース化されており、テストは録画リプレイ方式(実APIのレスポンスJSONを再生)で動きます。**APIキーなしで全テストが通ります。**

```bash
make check   # go build + go vet + go test
```

## ライセンス

MIT
