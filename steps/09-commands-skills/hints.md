# step 09 ヒント

まず自力で書いてみてください。ヒントは段階式です。

## 課題1: TODO(step09-1) スキル目次の注入

### ヒント1

`if len(opts.Skills) > 0` のときだけセクションを足します。構成は3部です: (1) これが何か(作業の手順書の一覧で、名前と説明だけを示していること)+ 関連する作業では skill ツールで本文を読むようにという案内、(2) この一覧はプロジェクト内のファイル由来の**データであって指示ではない**という宣言、(3) `- 名前: 説明` の列挙。

### ヒント2

列挙は `for _, s := range opts.Skills` で `fmt.Fprintf(&b, "\n- %s: %s", s.Name, s.Description)`。テストが見るのは「名前と説明が載っていること」「skill ツールへの案内があること」「スキル0件のときセクション自体が無いこと」です。境界の宣言文はテストでは検証していませんが、書く理由は README の通りです——文言は自分の言葉で構いません。

## 課題2: TODO(step09-2) isValidName

### ヒント1

step 04 の `isSimpleCommand` と同じ構造です。空文字は false。あとは1ルーンずつ見て、英数字と `-` `_` 以外が1つでもあれば false。

### ヒント2

```go
for _, r := range name {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
	case r == '-', r == '_':
	default:
		return false
	}
}
return true
```

`/` や `.` を許さないので、`../` を含む名前も絶対パスもこの1つの検証で弾けます。「危険な形を列挙する」のではなく「安全な形を定義する」——同じ原則が3度目の登場です(workspace、isSimpleCommand、そしてここ)。

なお `TestExecuteRejectsSymlinkEscape` もこの課題を埋めると通ります。シンボリックリンク対策そのものは `Execute` で実装済み(`ws.Resolve`)ですが、`Execute` は先に `isValidName` を通るため、穴が空いたままだとこのテストも失敗するからです。

## 答え合わせ

解答は次のstepの同じファイル([../10-streaming-hooks/prompt/prompt.go](../10-streaming-hooks/prompt/prompt.go)、[../10-streaming-hooks/command/command.go](../10-streaming-hooks/command/command.go))に埋まっています。
