package command

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/toshi0607/go-coding-agent-handson/steps/09-commands-skills/tools"
)

// newRegistry は dir を作業ディレクトリとする Registry を作る。
func newRegistry(t *testing.T, dir string) *Registry {
	t.Helper()
	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return New(ws)
}

// writeCommand は .agent/commands/<name>.md を作る。
func writeCommand(t *testing.T, dir, name, content string) {
	t.Helper()
	cmdDir := filepath.Join(dir, CommandsDir)
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// linkCommand は .agent/commands/<name>.md を target へのシンボリックリンクにする。
func linkCommand(t *testing.T, dir, name, target string) {
	t.Helper()
	cmdDir := filepath.Join(dir, CommandsDir)
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(cmdDir, name+".md")); err != nil {
		t.Fatal(err)
	}
}

func TestIsCommand(t *testing.T) {
	r := newRegistry(t, t.TempDir())

	commands := []string{"/help", "/explain main.go", "/my-cmd", "/my_cmd2"}
	for _, line := range commands {
		if !r.IsCommand(line) {
			t.Errorf("コマンドと判定されるべき: %q", line)
		}
	}

	// スラッシュで始まっていても、コマンド名として妥当でなければ
	// 通常の入力としてLLMに渡す。絶対パスから始まる依頼が
	// 「そんなコマンドはありません」で飲み込まれるのを防ぐ。
	notCommands := []string{
		"普通の入力",
		"/Users/foo/main.go を読んで",
		"/etc/hosts の中身は?",
		"/",
		"/ 何か",
		"/../../etc/passwd",
	}
	for _, line := range notCommands {
		if r.IsCommand(line) {
			t.Errorf("コマンドではないと判定されるべき: %q", line)
		}
	}
}

// コマンド名はそのままファイルパスに連結される。
// 検証がないと作業ディレクトリの外を読める。
func TestExecuteRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.md")
	if err := os.WriteFile(secret, []byte("秘密のプロンプト"), 0o600); err != nil {
		t.Fatal(err)
	}

	workDir := filepath.Join(dir, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRegistry(t, workDir)
	for _, name := range []string{"../../secret", "../secret", "/etc/passwd"} {
		got := r.Execute("/" + name)
		if got.Prompt != "" {
			t.Errorf("パストラバーサルでファイルが読まれた: %q → %q", name, got.Prompt)
		}
	}
}

// コマンドファイル自体がシンボリックリンクのケース。
//
// コマンド名の検証(isValidName)は「パスを組み立てる前」の防御であり、
// 組み立てたパスが実際にどこを指すかは見ていない。作業ディレクトリに
//
//	.agent/commands/leak.md -> ~/.ssh/id_rsa
//
// を置かれると、/leak の実行でリンク先の中身がカスタムコマンドとして
// 展開され、そのままユーザー入力としてLLMに送られる——つまり
// APIリクエストに載って外に出る。gitはシンボリックリンクを配布できるので、
// 悪意あるリポジトリをcloneして中でエージェントを動かすだけで成立する。
func TestExecuteRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windowsではシンボリックリンクの作成に特権が必要")
	}

	t.Run("外を指すリンク", func(t *testing.T) {
		outside := t.TempDir()
		secret := filepath.Join(outside, "id_rsa")
		if err := os.WriteFile(secret, []byte("-----BEGIN PRIVATE KEY-----"), 0o600); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		linkCommand(t, dir, "leak", secret)

		got := newRegistry(t, dir).Execute("/leak")
		if got.Prompt != "" {
			t.Errorf("作業ディレクトリ外のファイルが展開された: %q", got.Prompt)
		}
		if got.Output == "" {
			t.Error("拒否した理由が表示されていない")
		}
	})

	// 壊れたリンク(リンク先が存在しない)も拒否する。EvalSymlinks は
	// 「存在しない」と「壊れたリンク」で同じエラーを返すため、両者を
	// 同一視する実装だとリンク自身のパス(作業ディレクトリ内)が
	// 通ってしまい、あとから外にファイルが作られると読めるようになる。
	t.Run("外を指す壊れたリンク", func(t *testing.T) {
		outside := t.TempDir()
		notYet := filepath.Join(outside, "later.md") // まだ存在しない
		dir := t.TempDir()
		linkCommand(t, dir, "leak", notYet)

		got := newRegistry(t, dir).Execute("/leak")
		if got.Prompt != "" {
			t.Errorf("壊れたリンク経由で展開された: %q", got.Prompt)
		}
	})

	// 内側を指すリンクは正当なので通ること。
	// 封じ込めが厳しすぎて普通の使い方ができないのも困る。
	t.Run("内側を指すリンクは許可", func(t *testing.T) {
		dir := t.TempDir()
		real := filepath.Join(dir, "shared-prompt.md")
		if err := os.WriteFile(real, []byte("共有プロンプト: $ARGUMENTS"), 0o644); err != nil {
			t.Fatal(err)
		}
		linkCommand(t, dir, "shared", real)

		got := newRegistry(t, dir).Execute("/shared main.go")
		if got.Prompt != "共有プロンプト: main.go" {
			t.Errorf("作業ディレクトリ内のリンクが拒否された: Prompt=%q Output=%q", got.Prompt, got.Output)
		}
	})
}

func TestBuiltinCommands(t *testing.T) {
	r := newRegistry(t, t.TempDir())

	if got := r.Execute("/clear"); !got.Clear {
		t.Error("/clear は Clear フラグを立てるべき")
	}
	if got := r.Execute("/compact"); !got.Compact {
		t.Error("/compact は Compact フラグを立てるべき")
	}
	if got := r.Execute("/help"); !strings.Contains(got.Output, "/clear") {
		t.Errorf("/help の出力にコマンド一覧がない: %q", got.Output)
	}
}

func TestCustomCommand(t *testing.T) {
	dir := t.TempDir()
	writeCommand(t, dir, "explain", "次のファイルを日本語で解説してください: $ARGUMENTS")

	r := newRegistry(t, dir)
	got := r.Execute("/explain main.go")
	want := "次のファイルを日本語で解説してください: main.go"
	if got.Prompt != want {
		t.Errorf("got %q, want %q", got.Prompt, want)
	}

	// /help にカスタムコマンドも載る。
	if help := r.Execute("/help"); !strings.Contains(help.Output, "/explain") {
		t.Errorf("/help にカスタムコマンドが載っていない: %q", help.Output)
	}
}

func TestUnknownCommand(t *testing.T) {
	r := newRegistry(t, t.TempDir())
	got := r.Execute("/nosuch")
	if got.Prompt != "" {
		t.Error("未知のコマンドはLLMに送らない")
	}
	if got.Output == "" {
		t.Error("未知のコマンドはエラーメッセージを表示すべき")
	}
}
