# step 06 ヒント

まず自力で書いてみてください。ヒントは段階式です。課題1(agent/truncate.go)→ 課題2(agent/agent.go)の順に。

## 課題1: TODO(step06-1) truncateToolResult / truncateRunes

### ヒント1

先に `truncateRunes` から考えます。返すのは「先頭 max 文字」と「全体の文字数」の2つ。`for i, r := range s` の `i` は**ルーンの開始バイト位置**なので、「max 文字目に到達した瞬間の i」で `s[:i]` と切れば、バイト境界と文字境界が一致します。文字数のカウントはループの回った回数です。

`truncateToolResult` 側は、まず `len(result)`(バイト数)を上限と比べ、上限以下ならそのまま返します。1文字は1バイト以上なので、バイト数が上限以下なら文字数も上限以下で、文字を数える必要がありません。超えていたら `truncateRunes(result, maxChars)` を呼び、全体の文字数が上限以内なら `result` をそのまま返す(日本語のように1文字が複数バイトなら、バイト数が上限を超えても文字数は上限以内のことがあります)。超えていたら「先頭」+ 報告文(切り詰めたこと・上限・全体の文字数・続きが必要なら範囲を絞るよう案内)を返します。

### ヒント2

`truncateRunes` の実装イメージ:

```go
head = s
for i := range s {
	if total == max {
		head = s[:i]
	}
	total++
}
return head, total
```

`total == max` の判定を「代入だけ」にして total は数え続けるのがポイントです(全体の文字数も返したいので、途中で return しません)。上限ぴったりのときは head が書き換わらず、呼び出し側の「上限以内ならそのまま」に合流します。

報告文は `fmt.Sprintf` で組み立てます。配布状態の truncate.go は `fmt` を import していないので、import 文を足してください。報告文の例: `\n... (ツール結果が長すぎるため、先頭の N 文字だけを残して切り詰めました。全体は M 文字あります。続きが必要なら範囲を絞って取得してください)`。テストは「切り詰め」という語と、全体の長さが文字数(30)でありバイト数(90)でないことを見ています。

## 課題2: TODO(step06-2) 自動compactionの発動

### ヒント1

```go
if a.shouldCompact() {
	if err := a.Compact(ctx); err != nil {
		return "", err
	}
}
```

これを**どこに置くか**が問題です。置き場所は「ユーザー入力を履歴に積む前」= ターンの開始時。ツールループの中に置くと `TestCompactionHappensOnlyAtTurnBoundary` が落ちます。なぜ落ちるのかは README の compaction の節を読み直してください。

## 答え合わせ

解答は次のstepの同じファイル([../07-subagent/agent/truncate.go](../07-subagent/agent/truncate.go)、[../07-subagent/agent/agent.go](../07-subagent/agent/agent.go))に埋まっています。
