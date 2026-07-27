# step 02 ヒント

まず自力で書いてみてください。ヒントは段階式です。

## 課題1: TODO(step02-1) read_file.Run

### ヒント1

やることは3つだけです。(1) `input` を `json.Unmarshal` で無名structにパースする、(2) `filepath.Join(r.workDir, path)` でパスを組み立てる、(3) `os.ReadFile` で読んで `string(data)` を返す。import に `os` と `path/filepath` を足すのを忘れずに。

### ヒント2

パース用のstructはこう書きます:

```go
var in struct {
	Path string `json:"path"`
}
```

パースに失敗したときは、その旨をエラーで返します(`fmt.Errorf("入力のパースに失敗: %w", err)` など)。読み取りエラーは加工せずそのまま返して構いません——エラーメッセージ自体がLLMへの有用なフィードバックになります。

## 課題2: TODO(step02-2) エージェントループ

### ヒント1

処理の骨組みから考えます。まず `toolUses := toolUseBlocks(resp)` で tool_use ブロックを取り出します。そのうえで:

- 「ループを終えるか」の判定は `resp.StopReason != anthropic.StopReasonToolUse`
- 終える場合でも、`toolUses` が残っていたら**そのまま返ってはいけない**(READMEのケース1)
- 続ける場合は、各ブロックを `a.executeTool(ctx, block)` で実行し、結果を集めて履歴に積む

### ヒント2

より具体的な形にします。

```
toolUses := toolUseBlocks(resp)

if ターンの終わり(StopReason が ToolUse でない) {
    if len(toolUses) > 0 {
        // 各 tool_use に対して「中断された」旨の is_error 付き tool_result を作り、
        // anthropic.NewToolResultBlock(block.ID, メッセージ, true) で
        // 1つの user メッセージにまとめて履歴に積む
    }
    return textOf(resp), nil
}

if len(toolUses) == 0 {
    // 矛盾応答(ケース2)。空メッセージを送らずここで打ち切る
    return textOf(resp), nil
}

// 全ブロックを executeTool して、結果を anthropic.NewUserMessage(results...) に
// まとめて1件だけ履歴に積み、次のイテレーションへ
```

`results` の型は `[]anthropic.ContentBlockParamUnion` です。

## 答え合わせ

解答は次のstepの同じファイル([../03-file-tools/agent/agent.go](../03-file-tools/agent/agent.go)、[../03-file-tools/tools/read_file.go](../03-file-tools/tools/read_file.go))に埋まっています。
