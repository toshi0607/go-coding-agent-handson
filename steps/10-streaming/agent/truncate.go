package agent

// ツール結果の切り詰め。コンテキスト管理の全体像は compact.go の冒頭を参照。

import "fmt"

// defaultMaxToolResultChars はツール結果1件あたりの文字数上限のデフォルト。
// 巨大ファイルの read_file やビルドログの暴発からコンテキストを守る。
const defaultMaxToolResultChars = 20_000

// truncateToolResult はツール結果が maxChars 文字を超える場合に切り詰める。
// エージェントループ(executeTool)が、すべてのツールの結果に適用する。
//
// 戦略は「先頭を残して後ろを捨てる」。エラーメッセージやファイルの
// 重要情報は先頭に現れることが多いためだが、これは唯一の正解ではない。
// テストログは末尾にこそ結果が出るので「先頭+末尾を残す」戦略もありうるし、
// 実際のエージェントには要約をかけるものもある。何をどう捨てるかは
// エージェント設計の立派な判断ポイントである。
//
// 切り詰めたことは必ずLLMに伝える。黙って切ると、LLMは「全部読めた」と
// 誤解したまま推論を進めてしまう。
//
// 数えるのはバイトではなく文字(ルーン)である。Goの文字列はUTF-8の
// バイト列なので `result[:20000]` と書くと日本語やソースコード中の
// マルチバイト文字の途中で切れ、壊れたバイトがそのままAPIに送られる。
// 「文字数で切り詰めた」と言いながらバイト数で切るのは、日本語を
// 扱うツールでは実害のある嘘になる。
func truncateToolResult(result string, maxChars int) string {
	// 1文字は1バイト以上なので、バイト数が上限以下なら文字数も上限以下。
	// その場合は文字を数えずにそのまま返す。
	if len(result) <= maxChars {
		return result
	}
	head, total := truncateRunes(result, maxChars)
	if total <= maxChars {
		return result
	}
	return head +
		fmt.Sprintf("\n... (ツール結果が長すぎるため、先頭の %d 文字だけを残して切り詰めました。全体は %d 文字あります。続きが必要なら範囲を絞って取得してください)", maxChars, total)
}

// truncateRunes は s の先頭 max 文字と、s 全体の文字数を返す。
//
// []rune への変換ではなく range で走査しているのは、ツール結果が
// 数MBになりうるため。range over string はルーンの開始バイト位置を
// 返すので、そこで切ればバイト境界とルーン境界が一致する。
func truncateRunes(s string, max int) (head string, total int) {
	head = s
	for i := range s {
		if total == max {
			head = s[:i]
		}
		total++
	}
	return head, total
}
