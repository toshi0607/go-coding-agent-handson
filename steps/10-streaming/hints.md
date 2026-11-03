# step 10 ヒント

まず自力で書いてみてください。ヒントは段階式です。

## 課題1: TODO(step10-1) Stream の受信処理

### ヒント1

骨組みは次の4段です。

```go
var message anthropic.Message
stopped := false
for stream.Next() {
	event := stream.Current()
	// (1) message.Accumulate(event) — エラーなら返す
	// (2) event が text_delta なら onText に流す(onText nil チェックを忘れずに)
	//     event が message_stop なら stopped = true
}
// (3) stream.Err() を確認する
// (4) stopped が false なら io.ErrUnexpectedEOF を包んで返し、true なら &message を返す
```

### ヒント2

text_delta の判別は型スイッチ2段です。`event.AsAny()` が `anthropic.ContentBlockDeltaEvent` のとき、その `Delta.AsAny()` が `anthropic.TextDelta` ならテキスト差分です。同じ型スイッチに `anthropic.MessageStopEvent` の case も置けます。

```go
switch eventVariant := event.AsAny().(type) {
case anthropic.ContentBlockDeltaEvent:
	if delta, ok := eventVariant.Delta.AsAny().(anthropic.TextDelta); ok && onText != nil {
		onText(delta.Text)
	}
case anthropic.MessageStopEvent:
	stopped = true
}
```

input_json_delta はこの型アサーションに一致しないので、自然に「蓄積はされるが表示はされない」挙動になります。

## 答え合わせ

解答は次のstepの同じファイル([../11-hooks/llm/anthropic.go](../11-hooks/llm/anthropic.go))に埋まっています。
