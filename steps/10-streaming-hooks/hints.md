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

## 課題2: TODO(step10-2) RunPre の実行と判定

### ヒント1

まず payload を組み立てて実行します。

```go
p := payload{HookEventName: "PreToolUse", ToolName: toolName, ToolInput: input}
stderr, exitCode, err := r.run(ctx, h.Command, p)
```

判定は4分岐です: err がある / exitCode == 0 / exitCode == blockExitCode / それ以外。「どれを通し、どれをブロックするか」は README の fail-closed の節どおりに。

### ヒント2

- `err != nil` → 「フックを実行できなかったため安全側に倒して中止」の趣旨でエラーを返す(fail-closed)
- `exitCode == 0` → 何もしない(次のフックのループへ)
- `exitCode == blockExitCode` → `bytes.TrimSpace(stderr)` を理由に含めてエラーを返す。この文字列が is_error 付き tool_result としてLLMに渡り、LLMは別の方法を検討できる
- それ以外 → 「予期しない終了コード(契約は 0=許可 / 2=ブロック)。判定できないため中止」の趣旨でブロック

Goの `switch { case ...: }` で書くと4分岐が上から読める形になります。`fmt` の import を忘れずに。

## 答え合わせ

このstepの解答は [final/](../../final/) そのものです。[final/llm/anthropic.go](../../final/llm/anthropic.go) と [final/hooks/hooks.go](../../final/hooks/hooks.go) を突き合わせてください。
