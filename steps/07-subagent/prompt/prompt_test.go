package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildIncludesEnvironmentInfo(t *testing.T) {
	dir := t.TempDir()
	got := Build(Options{WorkDir: dir})
	if !strings.Contains(got, dir) {
		t.Error("作業ディレクトリがプロンプトに含まれていない")
	}
}

func TestBuildIncludesMemoryFile(t *testing.T) {
	dir := t.TempDir()
	memory := "- テストは go test ./... で実行する\n- コミットメッセージは日本語"
	if err := os.WriteFile(filepath.Join(dir, MemoryFileName), []byte(memory), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Build(Options{WorkDir: dir})
	if !strings.Contains(got, memory) {
		t.Error("CLAUDE.md の内容がプロンプトに含まれていない")
	}
}

func TestBuildWithoutMemoryFile(t *testing.T) {
	got := Build(Options{WorkDir: t.TempDir()})
	if strings.Contains(got, "プロジェクトメモリ") {
		t.Error("CLAUDE.md がないのにメモリセクションが含まれている")
	}
}
