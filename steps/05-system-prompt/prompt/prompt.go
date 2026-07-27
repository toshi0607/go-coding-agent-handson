// Package prompt はシステムプロンプトを組み立てる。
//
// システムプロンプトはエージェントの「役割定義書」で、3層で構成する:
//  1. 役割と行動方針(固定): エージェントとしての振る舞い方
//  2. 環境情報(起動時に注入): 作業ディレクトリ、OS、日付など。
//     LLMは実行環境を知らないので、教えなければ「あなたの環境によります」
//     のような答えしか返せない
//  3. プロジェクトメモリ(CLAUDE.md): プロジェクト固有の約束事。
//     ユーザーが毎回説明しなくても、エージェントが従うべき規約を
//     ファイルとしてリポジトリに置いておける
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// MemoryFileName はプロジェクトメモリのファイル名。
const MemoryFileName = "CLAUDE.md"

const basePrompt = `あなたはターミナルで動くコーディングエージェントです。
ユーザーの依頼に対し、必要ならツールを使ってファイルの調査・編集・コマンド実行を行い、結果を簡潔に報告してください。

行動方針:
- 推測でファイルの内容を語らない。必ず read_file で実物を確認してから答える
- ファイルを編集する前に、対象ファイルを読んで現状を把握する
- コードを変更したら、可能ならビルドやテストで動作を確認する
- 回答は簡潔に。長い前置きや、聞かれていないことの説明はしない`

// Options は Build に渡す情報。
type Options struct {
	// WorkDir は作業ディレクトリ(CLAUDE.md の探索場所)。
	WorkDir string
}

// Build はシステムプロンプトを組み立てる。
func Build(opts Options) string {
	var b strings.Builder
	b.WriteString(basePrompt)

	fmt.Fprintf(&b, `

環境情報:
- 作業ディレクトリ: %s
- OS: %s
- 今日の日付: %s`,
		opts.WorkDir, runtime.GOOS, time.Now().Format("2006-01-02"))

	// CLAUDE.md があれば「プロジェクトメモリ」として全文を注入する。
	// なければ何も足さない(存在しないセクションでプロンプトを汚さない)。
	memory, err := os.ReadFile(filepath.Join(opts.WorkDir, MemoryFileName))
	if err == nil && len(memory) > 0 {
		fmt.Fprintf(&b, `

プロジェクトメモリ(%s):
このプロジェクトで作業する際は、以下の内容に従ってください。
%s`, MemoryFileName, string(memory))
	}

	return b.String()
}
