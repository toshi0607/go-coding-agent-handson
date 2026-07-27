# step 08 ヒント

まず自力で書いてみてください。ヒントは段階式です。

## 課題1: TODO(step08-1) call の応答待ちループ

### ヒント1

骨組みは「無限ループ + select」です。

```go
for {
	select {
	case <-ctx.Done():
		// タイムアウト or キャンセル。サーバー名とメソッド名入りのエラー
	case msg, ok := <-c.lines:
		// ここで msg を処理する
	}
}
```

`c.lines` から受け取った `msg` の処理は、TODOコメントの仕様を上から順に if で書いていけばそのまま形になります。「読み飛ばす」は `continue` です。

### ヒント2

msg の処理の順序:

1. `!ok` → `c.lines` が閉じた = 接続断。エラーを返す
2. `msg.err != nil` → そのまま返す
3. `len(msg.line) == 0` → `continue`
4. `json.Unmarshal(msg.line, &resp)`(`resp` は `response` 型)。失敗したらエラー
5. `resp.ID == nil || *resp.ID != id` → 通知か別リクエストへの応答。`continue`
6. `resp.Error != nil` → それを返す(`*rpcError` は error を実装済み)
7. `result != nil` なら `json.Unmarshal(resp.Result, result)` の結果を、そうでなければ `nil` を返す

タイムアウトのエラーには `%w` で `ctx.Err()` を包んでおくと、呼び出し側が `context.DeadlineExceeded` を判別できます。

## 答え合わせ

解答は次のstepの同じファイル([../09-commands-skills/mcp/client.go](../09-commands-skills/mcp/client.go))に埋まっています。
