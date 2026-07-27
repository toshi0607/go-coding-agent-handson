package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsCommand(t *testing.T) {
	r := New(t.TempDir())

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

	r := New(filepath.Join(dir, "workdir"))
	for _, name := range []string{"../../secret", "../secret", "/etc/passwd"} {
		got := r.Execute("/" + name)
		if got.Prompt != "" {
			t.Errorf("パストラバーサルでファイルが読まれた: %q → %q", name, got.Prompt)
		}
	}
}

func TestBuiltinCommands(t *testing.T) {
	r := New(t.TempDir())

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
	cmdDir := filepath.Join(dir, CommandsDir)
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "次のファイルを日本語で解説してください: $ARGUMENTS"
	if err := os.WriteFile(filepath.Join(cmdDir, "explain.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(dir)
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
	r := New(t.TempDir())
	got := r.Execute("/nosuch")
	if got.Prompt != "" {
		t.Error("未知のコマンドはLLMに送らない")
	}
	if got.Output == "" {
		t.Error("未知のコマンドはエラーメッセージを表示すべき")
	}
}
