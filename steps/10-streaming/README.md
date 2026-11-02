# step 10: ストリーミング — 生成されたそばから表示する

機能はすでに出揃っているので、ここで足すのは**体験**です。

ここまでのエージェントは、LLMの応答が全部生成し終わってから一気に表示していました。長い応答では数十秒の沈黙が続きます。生成されたそばからテキストを流すように変えます。

## この章で学ぶこと

- SSEイベントの列から完全なレスポンスを組み立て直すこと(蓄積と表示の役割分担)

## 前のstepから引き継いだもの

step 09の解答(コマンドとスキル)は埋まった状態で始まります。今回増えたのは:

- [llm/client.go](llm/client.go) — `Client` に `Stream` が加わった。エージェントループは `Complete` から `Stream` に乗り換え済み
- [llm/anthropic.go](llm/anthropic.go) — 実APIのストリーミング(課題)
- [agent/agent.go](agent/agent.go) — `Stream` の呼び出しと、`onText` での画面表示(実装済み)

## ストリーミング: 蓄積と表示は別物

ストリーミングでは、レスポンスがSSEイベントの列に分解されて届きます。

```
message_start → content_block_start → content_block_delta ... → message_stop
```

実装の鍵は**役割分担**です。画面に流すのはテキスト差分(text_delta)だけですが、会話履歴に積むのは**完全なレスポンス**でなければなりません。tool_use ブロックの引数も、細切れのJSON断片(input_json_delta)として届きます——これを画面に流しても意味不明なだけです。

そこで `Stream` は2つの仕事を並行して行います。すべてのイベントをSDKの `Message.Accumulate` で完全なメッセージに組み立て直しながら、text_delta だけを `onText` コールバックに流す。戻り値の完全なメッセージを履歴に積むのはエージェントループの仕事で、`onText` はあくまで表示用です。

`llm.Client` をインターフェースにしておいた効能がここでも出ます。`ReplayClient` にも `Stream` を実装してあります(全文を一度に `onText` へ流すだけ)が、エージェントループのテストは何も変わらずに通ります。

## 課題

**TODO(step10-1)** [llm/anthropic.go](llm/anthropic.go) — `Stream` の受信処理(蓄積 + text_delta の抽出)

## 検証

```bash
make check STEP=10
```

ストリーミングのテストは `httptest` の偽SSEサーバーで、実APIなしで検証します(text_delta の分割結合、input_json_delta の蓄積、後乗せの stop_reason まで)。

## 動かしてみよう(任意)

```bash
export ANTHROPIC_API_KEY=sk-ant-...
cd example
go run ../steps/10-streaming
```

応答が流れるように表示されることを確かめてください。

## 次のstepへ

最後のstep 11では、ツール実行の前後に外部コマンドを差し込める hooks を実装します。エージェント本体を変更せずに、検査やログ記録を追加できる拡張点です。
