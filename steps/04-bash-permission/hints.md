# step 04 ヒント

まず自力で書いてみてください。ヒントは段階式です。課題は 04-1 → 04-2 → 04-5 → 04-4 → 04-3 の順に進めると、テストの失敗が1つずつ減っていきます。

## 課題1: TODO(step04-1) isSimpleCommand

### ヒント1

「通してよい文字」の集合を正規表現の文字クラス `[...]` で書き、文字列の先頭(`^`)から末尾(`$`)までがその文字だけでできているかを判定します。通してよいのは、英数字(a-z, A-Z, 0-9)と、`スペース - _ . / = : , + @` の記号だけです。Goでは `regexp` パッケージを使います。

### ヒント2

正規表現はパッケージレベルの変数で1度だけコンパイルし(`regexp.MustCompile`)、関数の中では `MatchString` を呼ぶだけにします。

```go
var simpleCommand = regexp.MustCompile(`^[A-Za-z0-9ここに許可する記号を並べる]*$`)

func isSimpleCommand(command string) bool {
	return simpleCommand.MatchString(command)
}
```

- `-` は文字クラスの末尾に置くと、範囲指定ではなく `-` そのものになります
- 繰り返しは `*`(0文字以上)で構いません
- 日本語などのマルチバイト文字は文字クラスに含まれないので false になります(安全側)
- Go の `regexp` は RE2 方式で、照合は入力の長さに比例する時間で終わります。LLMが長いコマンドを送ってきても判定が固まることはありません

## 課題2: TODO(step04-2) checkBash

### ヒント1

順序が命です。**最初に** `isSimpleCommand` を確認し、単純でなければ allowlist を見ずに `c.confirm` へ。allowlist の照合はそのあとです。順序を逆にすると `ls; rm -rf /` が「先頭は ls」で素通りします。

### ヒント2

- 単純でない場合: `c.confirm(質問文, "", "コマンド")`(sessionKey は空 = 「a」でも記憶しない)
- 単純な場合: `head := commandHead(in.Command)` を取り、`c.bashAllowlist[head] || c.sessionAllowed["bash:"+head]` なら `nil` を返す
- それ以外: `c.confirm(質問文, "bash:"+head, "コマンド")`
- 質問文にはコマンド文字列そのものを含めます(例: `fmt.Sprintf("コマンドの実行を許可しますか？\n  $ %s", in.Command)`)

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
