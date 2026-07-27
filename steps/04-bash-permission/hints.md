# step 04 ヒント

まず自力で書いてみてください。ヒントは段階式です。課題は 04-1 → 04-2 → 04-5 → 04-4 → 04-3 の順に進めると、テストの失敗が1つずつ減っていきます。

## 課題1: TODO(step04-1) isSimpleCommand

### ヒント1

`for _, r := range command` で1文字(ルーン)ずつ見て、「通してよい文字」のどれにも当てはまらないルーンが1つでもあれば false です。通してよいのは、英数字(a-z, A-Z, 0-9)と、`スペース - _ . / = : , + @` の記号だけです。

### ヒント2

Goの `switch` は case に条件式と複数の値を並べられます。

```go
switch {
case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
	// 英数字はOK
case /* 許可する記号たち */:
default:
	return false
}
```

日本語などのマルチバイト文字も default に落ちて false になります(安全側)。

## 課題2: TODO(step04-2) checkBash

### ヒント1

順序が命です。**最初に** `isSimpleCommand` を確認し、単純でなければ allowlist を見ずに `c.confirm` へ。allowlist の照合はそのあとです。順序を逆にすると `ls; rm -rf /` が「先頭は ls」で素通りします。

### ヒント2

- 単純でない場合: `c.confirm(質問文, "", "コマンド")`(sessionKey は空 = 「a」でも記憶しない)
- 単純な場合: `head := commandHead(in.Command)` を取り、`c.bashAllowlist[head] || c.sessionAllowed["bash:"+head]` なら `nil` を返す
- それ以外: `c.confirm(質問文, "bash:"+head, "コマンド")`
- 質問文にはコマンド文字列そのものを含めます(例: `fmt.Sprintf("コマンドの実行を許可しますか?\n  $ %s", in.Command)`)

## 課題3: TODO(step04-5) 権限チェックの挿入(agent/agent.go)

### ヒント1

場所は「ツールの解決後、`tool.Run` の前」。`a.perm.Check(block.Name, block.Input)` がエラーを返したら、`anthropic.NewToolResultBlock(block.ID, err.Error(), true)` を返します(true = is_error)。`executeTool` 内の他のエラー処理と同じ形です。

## 課題4: TODO(step04-4) contains

### ヒント1

`filepath.Rel(w.root, path)` で root からの相対パスを計算します。結果が `..` そのもの、または `../` で始まるなら外側です。`.`(root自身)は内側です。

### ヒント2

```go
rel, err := filepath.Rel(w.root, path)
if err != nil {
	return false
}
```

のあと、`rel == "."` は true、`rel == ".."` は false、`strings.HasPrefix(rel, ".."+string(filepath.Separator))` も false。それ以外は true です。`strings` の import を忘れずに。

## 課題5: TODO(step04-3) Resolve

### ヒント1

手順はTODOコメントの4段階そのままです。

```go
abs := path
if !filepath.IsAbs(abs) {
	abs = filepath.Join(w.root, abs)
}
abs = filepath.Clean(abs)
```

そのあと `evalSymlinksLenient(abs)` を呼び、エラーはそのまま返します。最後に `w.contains(resolved)` が false なら、「どのパスが作業ディレクトリ(w.root)の外か」が分かるエラーを返します。

## 答え合わせ

解答は次のstepの同じファイル([../05-system-prompt/permission/permission.go](../05-system-prompt/permission/permission.go)、[../05-system-prompt/tools/workspace.go](../05-system-prompt/tools/workspace.go)、[../05-system-prompt/agent/agent.go](../05-system-prompt/agent/agent.go))に埋まっています。
