// Package command はスラッシュコマンドを実装する。
//
// スラッシュコマンドには2種類ある:
//   - 組み込みコマンド(/help, /clear, /compact):
//     LLMを介さず、エージェントプログラム自体を操作する
//   - カスタムコマンド(.agent/commands/<名前>.md):
//     よく使うプロンプトに名前を付けてファイルとして保存したもの。
//     実体は「プロンプトのパッケージ化」であり、実行するとファイルの
//     中身が展開されて、通常のユーザー入力としてLLMに送られる
//
// 「LLMに渡る前に展開されるだけ」という点が肝で、コマンドと言っても
// エージェント側に新しい能力が生えるわけではない。ただし定型作業を
// 1語で呼び出せることの実用価値は大きい。
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CommandsDir はカスタムコマンドの置き場所(作業ディレクトリ基準)。
const CommandsDir = ".agent/commands"

// argumentsPlaceholder はカスタムコマンド内で引数に置き換わる目印。
const argumentsPlaceholder = "$ARGUMENTS"

// Result はコマンド実行の結果。
type Result struct {
	// Output はユーザーに表示するメッセージ(組み込みコマンド用)。
	Output string
	// Prompt が非空なら、これをユーザー入力としてLLMに送る
	// (カスタムコマンドの展開結果)。
	Prompt string
	// Clear は会話履歴のクリア指示(/clear)。
	Clear bool
	// Compact はコンテキスト圧縮指示(/compact)。
	Compact bool
}

// Registry はコマンドの解決と実行を担う。
type Registry struct {
	dir string
}

// New は workDir を基準にカスタムコマンドを探す Registry を作る。
func New(workDir string) *Registry {
	return &Registry{dir: filepath.Join(workDir, CommandsDir)}
}

// IsCommand は入力がスラッシュコマンドかどうかを判定する。
func (r *Registry) IsCommand(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "/")
}

// Execute はコマンドを実行する。
func (r *Registry) Execute(line string) Result {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "/"))
	name, args, _ := strings.Cut(line, " ")

	switch name {
	case "help":
		return Result{Output: r.helpText()}
	case "clear":
		return Result{Clear: true, Output: "会話履歴をクリアしました。"}
	case "compact":
		return Result{Compact: true}
	}

	// 組み込みになければカスタムコマンドを探す。
	content, err := os.ReadFile(filepath.Join(r.dir, name+".md"))
	if err != nil {
		return Result{Output: fmt.Sprintf("コマンド /%s は存在しません。/help で一覧を確認できます。", name)}
	}
	// $ARGUMENTS をコマンドの引数部分で置き換えてプロンプトにする。
	prompt := strings.ReplaceAll(string(content), argumentsPlaceholder, strings.TrimSpace(args))
	return Result{Prompt: prompt}
}

func (r *Registry) helpText() string {
	var b strings.Builder
	b.WriteString(`組み込みコマンド:
  /help     このヘルプを表示
  /clear    会話履歴をクリア
  /compact  会話履歴を要約して圧縮`)

	if custom := r.customCommandNames(); len(custom) > 0 {
		b.WriteString("\n\nカスタムコマンド(" + CommandsDir + "/):")
		for _, name := range custom {
			b.WriteString("\n  /" + name)
		}
	}
	return b.String()
}

// customCommandNames は .agent/commands/*.md のコマンド名一覧を返す。
func (r *Registry) customCommandNames() []string {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	sort.Strings(names)
	return names
}
