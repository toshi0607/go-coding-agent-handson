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

// 段階的開示の検証。スキルは名前と説明だけを載せ、本文は載せない。
// ここが逆になると、スキルを増やすほど毎ターンのコストが膨らむ。
func TestBuildIncludesSkillIndexOnly(t *testing.T) {
	got := Build(Options{
		WorkDir: t.TempDir(),
		Skills: []SkillSummary{
			{Name: "commit-message", Description: "コミットメッセージの書き方"},
			{Name: "code-review", Description: "レビューの観点"},
		},
	})

	for _, want := range []string{"commit-message", "コミットメッセージの書き方", "code-review", "レビューの観点"} {
		if !strings.Contains(got, want) {
			t.Errorf("スキルの目次に %q がない", want)
		}
	}
	// 本文の読み込み方法(skillツール)を案内していること。
	if !strings.Contains(got, "skill") {
		t.Error("skillツールの案内がない")
	}
}

func TestBuildWithoutSkills(t *testing.T) {
	got := Build(Options{WorkDir: t.TempDir()})
	if strings.Contains(got, "利用可能なスキル") {
		t.Error("スキルがないのにスキルセクションが含まれている")
	}
}
