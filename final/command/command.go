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
// ミニClaude Codeに新しい機能が増えるわけではない。ただし定型作業を
// 1語で呼び出せることの実用価値は大きい。
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/toshi0607/go-coding-agent-handson/final/tools"
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
	// ws はコマンドファイルのパス解決に使う。
	//
	// カスタムコマンドは「リポジトリに置かれたファイルを読んで、その中身を
	// ユーザー入力としてLLMに送る」機能である。つまりファイルの中身は
	// そのままAPIリクエストに載って外に出る。コマンド名を検証しても、
	// コマンドファイル自身がシンボリックリンクなら
	//
	//	.agent/commands/leak.md -> ~/.ssh/id_rsa
	//
	// のように作業ディレクトリの外を読める。名前の検証(isValidName)は
	// 「パスを組み立てる前」の防御で、「組み立てたパスが実際にどこを指すか」は
	// 別の防御(Workspace)が要る、ということである。
	ws *tools.Workspace
}

// New はカスタムコマンドを作業ディレクトリ内から探す Registry を作る。
func New(ws *tools.Workspace) *Registry {
	return &Registry{ws: ws}
}

// isValidName はコマンド名として許可する形を定める。
//
// 名前はそのままファイルパスの一部になる(.agent/commands/<名前>.md)。
// 検証せずに連結すると `/../../../etc/passwd` のような入力で
// 作業ディレクトリの外を読めてしまう。ここは「危険な文字を弾く」のでは
// なく「安全な文字だけ通す」——ファイル操作の範囲を作業ディレクトリに限定する(tools.Workspace)と
// 同じ考え方である。
func isValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Execute は入力がスラッシュコマンドなら実行して、結果と true を返す。
// スラッシュコマンドでなければゼロ値の Result と false を返すので、
// 呼び出し側はその入力を通常の依頼としてLLMに渡す。
//
// 「/ で始まる」だけでは足りない。ユーザーは
//
//	/Users/foo/main.go を読んで
//
// のように、絶対パスから始まる依頼を書くことがある。これをスラッシュ
// コマンドとして扱うと「スラッシュコマンド /Users/foo/main.go は存在しません」
// と表示されて依頼がLLMに届かない。そこで / の後ろ(最初の空白まで)が
// コマンド名として妥当かどうかまで見て、妥当でなければスラッシュコマンド
// ではないと判断する。
//
// 判定と実行の入口をこの1つにまとめているので、名前の検証(isValidName)は
// パスを組み立てる前に必ず1回だけ通る。判定と実行が別々のメソッドだと、
// 判定を呼ばずに実行だけを呼ぶ経路で検証が抜けうる。
func (r *Registry) Execute(line string) (Result, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return Result{}, false
	}
	name, args, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	if !isValidName(name) {
		return Result{}, false
	}

	switch name {
	case "help":
		return Result{Output: r.helpText()}, true
	case "clear":
		return Result{Clear: true, Output: "会話履歴をクリアしました。"}, true
	case "compact":
		return Result{Compact: true}, true
	}

	// 組み込みになければカスタムコマンドを探す。
	// パスは必ず Workspace 経由で解決する(上の ws のコメントを参照)。
	path, err := r.ws.Resolve(filepath.Join(CommandsDir, name+".md"))
	if err != nil {
		return Result{Output: fmt.Sprintf("スラッシュコマンド /%s は読み込めません: %v", name, err)}, true
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Result{Output: fmt.Sprintf("スラッシュコマンド /%s は存在しません。/help で一覧を確認できます。", name)}, true
	}
	// $ARGUMENTS をコマンドの引数部分で置き換えてプロンプトにする。
	prompt := strings.ReplaceAll(string(content), argumentsPlaceholder, strings.TrimSpace(args))
	return Result{Prompt: prompt}, true
}

func (r *Registry) helpText() string {
	var b strings.Builder
	b.WriteString(`組み込みのスラッシュコマンド:
  /help     このヘルプを表示
  /clear    会話履歴をクリア
  /compact  会話履歴を要約して圧縮`)

	if custom := r.customCommandNames(); len(custom) > 0 {
		b.WriteString("\n\nカスタムスラッシュコマンド(" + CommandsDir + "/):")
		for _, name := range custom {
			b.WriteString("\n  /" + name)
		}
	}
	return b.String()
}

// customCommandNames は .agent/commands/*.md のコマンド名一覧を返す。
func (r *Registry) customCommandNames() []string {
	// 一覧を作るだけでも Workspace を通す。commands ディレクトリ自体が
	// 外を指すリンクなら、そこにあるファイル名を見せる必要はない。
	dir, err := r.ws.Resolve(CommandsDir)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	slices.Sort(names)
	return names
}
