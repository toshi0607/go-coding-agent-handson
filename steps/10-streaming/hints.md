# step 10 ヒント

まず自力で書いてみてください。ヒントは段階式です。

## 課題1: TODO(step10-1) Stream の受信処理

### ヒント1

骨組みは次の3段です。

```go
message := anthropic.Message{}
for stream.Next() {
	event := stream.Current()
	// (1) message.Accumulate(event) — エラーなら返す
	// (2) event が text_delta なら onText に流す(onText nil チェックを忘れずに)
}
// (3) stream.Err() を確認してから &message を返す
```

### ヒント2

text_delta の判別は型スイッチ2段です。`event.AsAny()` が `anthropic.ContentBlockDeltaEvent` のとき、その `Delta.AsAny()` が `anthropic.TextDelta` ならテキスト差分です。

```go
switch eventVariant := event.AsAny().(type) {
case anthropic.ContentBlockDeltaEvent:
	if delta, ok := eventVariant.Delta.AsAny().(anthropic.TextDelta); ok {
		onText(delta.Text)
	}
}
```

input_json_delta はこの型アサーションに一致しないので、自然に「蓄積はされるが表示はされない」挙動になります。

## 答え合わせ

解答は次のstepの同じファイル([../11-hooks/llm/anthropic.go](../11-hooks/llm/anthropic.go))に埋まっています。
