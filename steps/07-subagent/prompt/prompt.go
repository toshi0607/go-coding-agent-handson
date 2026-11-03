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
	"runtime"
	"strings"
	"time"

	"github.com/toshi0607/go-coding-agent-handson/steps/07-subagent/tools"
)

// MemoryFileName はプロジェクトメモリのファイル名。
const MemoryFileName = "CLAUDE.md"

const basePrompt = `あなたはターミナルで動くコーディングエージェントです。
ユーザーの依頼に対し、必要ならツールを使ってファイルの調査・編集・コマンド実行を行い、結果を簡潔に報告してください。

行動方針:
- コードについて答える前に、関係するファイルを read_file で読む。開いていないファイルの中身は推測で答えず、読んで確かめてから答える
- ファイルを編集する前に、そのファイルを読んで現状を把握する
- 頼まれた変更と、そのために必要な変更だけを行う。頼まれていない機能の追加やリファクタリングはしない
- コードを変更したら、可能ならビルドやテストで動作を確かめる
- 回答は簡潔にまとめる。ユーザーはターミナルで読むので、長い前置きや聞かれていない説明は省く`

// Options は Build に渡す情報。
type Options struct {
	// Workspace は作業ディレクトリ。CLAUDE.md もここを通して読む。
	Workspace *tools.Workspace
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
		opts.Workspace.Root(), runtime.GOOS, time.Now().Format("2006-01-02"))

	// CLAUDE.md があれば「プロジェクトメモリ」として全文を注入する。
	// なければ何も足さない(存在しないセクションでプロンプトを汚さない)。
	// CLAUDE.md が作業ディレクトリの外(~/.ssh/id_rsa など)を指すリンクなら
	// 読み込まない。ファイル系ツールと同じく Workspace でパスを解決する。
	path, err := opts.Workspace.Resolve(MemoryFileName)
	if err != nil {
		return b.String()
	}
	memory, err := os.ReadFile(path)
	if err == nil && len(memory) > 0 {
		fmt.Fprintf(&b, `

プロジェクトメモリ(%s):
このプロジェクトで作業する際は、以下の内容に従ってください。
%s`, MemoryFileName, string(memory))
	}

	return b.String()
}
