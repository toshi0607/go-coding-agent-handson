# step 03 ヒント

まず自力で書いてみてください。ヒントは段階式です。

## 課題1: TODO(step03-1) edit_file の編集本体

### ヒント1

処理は2段階に分かれます。

1. **読み取りと新規作成の判定**: `os.ReadFile(path)` の結果で分岐します。「存在しないエラー かつ old_str が空」なら新規作成、「存在しないエラー以外のエラー」ならそのまま返す、それ以外は編集に進む。存在しないことの判定は `errors.Is(err, os.ErrNotExist)` です
2. **置換**: `strings.Count(content, in.OldStr)` で出現回数を数え、0回・複数回・「old_strが空なのにファイルが有る」をそれぞれエラーに。ちょうど1回なら置換して書き戻します

### ヒント2

- 新規作成は `os.MkdirAll(filepath.Dir(path), 0o755)` してから `os.WriteFile(path, []byte(in.NewStr), 0o644)`
- 置換は `strings.Replace(content, in.OldStr, in.NewStr, 1)`(最後の引数 1 = 最初の1箇所だけ)
- Goの `switch { case 条件: ... }`(式なしswitch)を使うと、5分岐が上から順に読める形で書けます。`os.ReadFile` 直後の分岐と `strings.Count` の分岐で switch を2つに分けるときれいです
- エラーメッセージには「LLMが次に何をすべきか」を入れてください。見つからない → 「read_file で最新の内容を確認してください」、複数マッチ → 「前後の文脈を含めて一意にしてください」

## 答え合わせ

解答は次のstepの同じファイル([../04-bash-permission/tools/edit_file.go](../04-bash-permission/tools/edit_file.go))に埋まっています。ただし step 04 ではパス解決が `Workspace` 経由になり、新規作成が `O_CREATE|O_EXCL` に強化されています(理由は step 04 の README で説明します)。このstepの答え合わせとして見るのは、`os.ReadFile` 以降の分岐構造です。
