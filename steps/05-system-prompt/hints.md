# step 05 ヒント

まず自力で書いてみてください。ヒントは段階式です。

## 課題1: TODO(step05-1) 環境情報の注入

### ヒント1

`b` は `strings.Builder` なので、`fmt.Fprintf(&b, ...)` で追記できます。載せるのは作業ディレクトリ(`opts.WorkDir`)、OS(`runtime.GOOS`)、今日の日付です。「環境情報:」のような見出しを付け、`basePrompt` との間は空行で区切ると読みやすくなります。

### ヒント2

日付は `time.Now().Format("2006-01-02")`。Goのレイアウト文字列は「2006年1月2日15時4分5秒」という決まった日時で書式を表現します(`YYYY-MM-DD` ではありません)。import には `fmt` / `runtime` / `time` が要ります。

## 課題2: TODO(step05-2) CLAUDE.md の注入

### ヒント1

`os.ReadFile(filepath.Join(opts.WorkDir, MemoryFileName))` で読みます。エラーのときと中身が空のときは**何もしない**(エラーを返す必要すらありません——CLAUDE.md は任意ファイルです)。

### ヒント2

読めたときだけ、見出し(どのファイル由来か)+「このプロジェクトで作業する際は、以下の内容に従ってください」のような説明 + 全文、を追記します。`err == nil && len(memory) > 0` の形で判定すると、「読めたが空」も除外できます。

## 答え合わせ

解答は次のstepの同じファイル([../06-context/prompt/prompt.go](../06-context/prompt/prompt.go))に埋まっています。
