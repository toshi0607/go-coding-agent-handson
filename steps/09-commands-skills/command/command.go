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
	"slices"
	"strings"

	"github.com/toshi0607/go-coding-agent-handson/steps/09-commands-skills/tools"
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

// IsCommand は入力がスラッシュコマンドかどうかを判定する。
//
// 「/ で始まる」だけでは足りない。ユーザーは
//
//	/Users/foo/main.go を読んで
//
// のように、絶対パスから始まる依頼を書くことがある。これをコマンド
// 扱いすると「コマンド /Users/foo/main.go は存在しません」と言われて
// 依頼がLLMに届かない。コマンド名として妥当な形かどうかまで見る。
func (r *Registry) IsCommand(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return false
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	return isValidName(name)
}

// isValidName はコマンド名として許可する形を定める。
//
// TODO(step09-2): 実装する。
//
// 名前はそのままファイルパスの一部になる(.agent/commands/<名前>.md)。
// 検証せずに連結すると `/../../../etc/passwd` のような入力で
// 作業ディレクトリの外を読めてしまう。
//
// もう1つの罠は「/ で始まる入力は全部コマンド」という判定である。
// ユーザーは「/Users/foo/main.go を読んで」のように絶対パスから始まる
// 依頼を普通に書く。これをコマンド扱いすると「コマンド /Users/foo/main.go は
// 存在しません」と言われ、依頼がLLMに届かない。
//
// 対策はどちらも同じで、「危険な文字を弾く」のではなく「コマンド名として
// 安全な文字(英数字と - _)だけを通す」。ファイル操作の封じ込め
// (tools.Workspace)や bash の isSimpleCommand と同じ考え方である。
// 空文字も false にすること。
//
// なお、この検証だけでは足りない。コマンドファイル自身がシンボリック
// リンクなら、名前が妥当でも外を読めてしまう——だから Execute では
// 名前の検証に加えて Workspace でパスを解決している(実装済み)。
// 「パスを組み立てる前」と「組み立てたパスが実際にどこを指すか」は
// 別の防御である。
func isValidName(name string) bool {
	panic("TODO(step09-2): steps/09-commands-skills/command/command.go を実装してください(hints.md 参照)")
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

	// IsCommand で弾いているはずだが、Execute だけを呼ばれても
	// 安全であるようにここでも検証する。パスを組み立てる直前が、
	// 検証を置くべき場所である。
	if !isValidName(name) {
		return Result{Output: fmt.Sprintf("コマンド名として使えない文字が含まれています: /%s", name)}
	}

	// 組み込みになければカスタムコマンドを探す。
	// パスは必ず Workspace 経由で解決する(上の ws のコメントを参照)。
	path, err := r.ws.Resolve(filepath.Join(CommandsDir, name+".md"))
	if err != nil {
		return Result{Output: fmt.Sprintf("コマンド /%s は読み込めません: %v", name, err)}
	}
	content, err := os.ReadFile(path)
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
