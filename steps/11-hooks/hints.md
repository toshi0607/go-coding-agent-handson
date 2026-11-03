# step 11 ヒント

まず自力で書いてみてください。ヒントは段階式です。

## 課題1: TODO(step11-1) RunPre の実行と判定

### ヒント1

まず payload を組み立てて実行します。

```go
p := payload{HookEventName: "PreToolUse", ToolName: toolName, ToolInput: input}
stderr, exitCode, err := r.run(ctx, h.Command, p)
```

判定は4分岐です: err がある / exitCode == 0 / exitCode == blockExitCode / それ以外。「どれを通し、どれをブロックするか」は README の fail-closed の節どおりに。

### ヒント2

- `err != nil` → 「PreToolUseフックを実行できなかったため、ツールを実行しませんでした」の趣旨でエラーを返す(fail-closed)
- `exitCode == 0` → 何もしない(次のフックのループへ)
- `exitCode == blockExitCode` → `bytes.TrimSpace(stderr)` を理由に含めてエラーを返す。この文字列が is_error 付き tool_result としてLLMに渡り、LLMは別の方法を検討できる
- それ以外 → 「予期しない終了コード(0は許可、2はブロック)。判定できないため、ツールを実行しませんでした」の趣旨でブロック

Goの `switch { case ...: }` で書くと4分岐が上から読める形になります。`fmt` の import を忘れずに。

## 答え合わせ

このstepの解答は [final/](../../final/) そのものです。[final/hooks/hooks.go](../../final/hooks/hooks.go) と突き合わせてください。
