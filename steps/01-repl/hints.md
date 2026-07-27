# step 01 ヒント

まず自力で書いてみてください。ヒントは段階式で、後のヒントほど具体的になります。解答そのものはここには書きません。

## 課題1: TODO(step01-1) ユーザー入力を履歴に積む

### ヒント1

`a.history` は `[]anthropic.MessageParam` です。ここに「role が user で、本文が userInput のメッセージ」を1件 append します。

### ヒント2

SDKにヘルパーがあります。テキストブロックを作るのが `anthropic.NewTextBlock(text)`、それを user メッセージに包むのが `anthropic.NewUserMessage(blocks...)` です。組み合わせると1行になります。

## 課題2: TODO(step01-2) 応答を履歴に積む

### ヒント1

`resp` は `*anthropic.Message`(レスポンス型)で、履歴に入れるのは `anthropic.MessageParam`(リクエスト型)です。型が違うので、そのままは append できません。変換が必要です。

### ヒント2

`anthropic.Message` には、自分自身をリクエスト用の `MessageParam` に変換するメソッドが生えています。SDKのドキュメントか、`llm/replay_test.go` の `TestReplayMessageRoundTrip` を見てみてください。そこで使われているものがそれです。

## 答え合わせ

解答は次のstepの同じファイル([../02-read-tool/agent/agent.go](../02-read-tool/agent/agent.go))に埋まっています。step 02では `Run` がループに変わっていますが、この2箇所の積み方は同じです。
