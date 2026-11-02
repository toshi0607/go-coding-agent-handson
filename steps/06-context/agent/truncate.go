package agent

// ツール結果の切り詰め。コンテキスト管理の全体像は compact.go の冒頭を参照。

// defaultMaxToolResultChars はツール結果1件あたりの文字数上限のデフォルト。
// 巨大ファイルの read_file やビルドログの暴発からコンテキストを守る。
const defaultMaxToolResultChars = 20_000

// truncateToolResult はツール結果が maxChars 文字を超える場合に切り詰める。
// エージェントループ(executeTool)が、すべてのツールの結果に適用する。
//
// TODO(step06-1): 実装する。満たすべき仕様:
//
//   - maxChars 文字以内ならそのまま返す。1文字は1バイト以上
//     なので、先にバイト数(len(result))が上限以下かを比べ、以下なら
//     文字を数えずにそのまま返す(早期リターン)
//   - 超えていたら「先頭 maxChars 文字」を残して後ろを捨てる。
//     エラーメッセージやファイルの重要情報は先頭に現れることが多い
//     ためだが、これは唯一の正解ではない。テストログは末尾にこそ結果が
//     出るので「先頭+末尾を残す」戦略もありうるし、実際のエージェントには
//     要約をかけるものもある。何をどう捨てるかは設計の立派な判断ポイントだ
//   - 切り詰めたことと全体の長さを、切り詰めた結果の末尾に書き足して
//     LLMに必ず伝える。黙って切ると、LLMは「全部読めた」と誤解した
//     まま推論を進めてしまう
//   - 数えるのはバイトではなく文字(ルーン)。下の truncateRunes を
//     実装して使うこと
func truncateToolResult(result string, maxChars int) string {
	panic("TODO(step06-1): steps/06-context/agent/truncate.go を実装してください(hints.md 参照)")
}

// truncateRunes は s の先頭 max 文字と、s 全体の文字数を返す。
//
// TODO(step06-1続き): 実装する。
//
// Goの文字列はUTF-8のバイト列である。`s[:20000]` と書くと日本語や
// ソースコード中のマルチバイト文字の途中で切れ、壊れたバイト列が
// そのままAPIに送られる。「N文字で切り詰めた」と報告しながらバイト数で
// 切るのは、日本語を扱うツールでは実害のある嘘になる。
//
// []rune(s) に変換すれば文字単位で切れるが、ツール結果は数MBに
// なりうるので全体の変換コピーは避けたい。range over string が
// 「ルーンの開始バイト位置」を返すことを利用すると、コピーなしで
// バイト境界とルーン境界を一致させられる。
func truncateRunes(s string, max int) (head string, total int) {
	panic("TODO(step06-1続き): steps/06-context/agent/truncate.go を実装してください(hints.md 参照)")
}
