package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsCommand(t *testing.T) {
	r := New(t.TempDir())
	if !r.IsCommand("/help") {
		t.Error("/help はコマンドと判定されるべき")
	}
	if r.IsCommand("普通の入力") {
		t.Error("スラッシュで始まらない入力はコマンドではない")
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
