package prompt

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming/tools"
)

// newWorkspace は dir を作業ディレクトリにした Workspace を作る。
func newWorkspace(t *testing.T, dir string) *tools.Workspace {
	t.Helper()
	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestBuildIncludesEnvironmentInfo(t *testing.T) {
	ws := newWorkspace(t, t.TempDir())
	got := Build(Options{Workspace: ws})
	// t.TempDir() ではなく ws.Root() と比べる。macOS では t.TempDir() が
	// /var/... を返し、Root() はリンクを解決した /private/var/... を返す。
	if !strings.Contains(got, ws.Root()) {
		t.Error("作業ディレクトリがプロンプトに含まれていない")
	}
}

func TestBuildIncludesMemoryFile(t *testing.T) {
	dir := t.TempDir()
	memory := "- テストは go test ./... で実行する\n- コミットメッセージは日本語"
	if err := os.WriteFile(filepath.Join(dir, MemoryFileName), []byte(memory), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Build(Options{Workspace: newWorkspace(t, dir)})
	if !strings.Contains(got, memory) {
		t.Error("CLAUDE.md の内容がプロンプトに含まれていない")
	}
}

// CLAUDE.md が作業ディレクトリの外を指すシンボリックリンクなら読み込まない。
// cloneしたリポジトリの CLAUDE.md が ~/.ssh/id_rsa へのリンクだった場合に、
// 秘密鍵の中身がシステムプロンプトに入るのを防ぐ。
func TestBuildSkipsMemoryFileOutsideWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windowsではシンボリックリンクの作成に特権が必要")
	}
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(secret, []byte("秘密鍵の中身"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, MemoryFileName)); err != nil {
		t.Fatal(err)
	}

	got := Build(Options{Workspace: newWorkspace(t, dir)})
	if strings.Contains(got, "秘密鍵の中身") {
		t.Error("作業ディレクトリの外を指す CLAUDE.md の内容がプロンプトに含まれている")
	}
	if strings.Contains(got, "プロジェクトメモリ") {
		t.Error("CLAUDE.md を読み込まなかったのにメモリセクションが含まれている")
	}
}

func TestBuildWithoutMemoryFile(t *testing.T) {
	got := Build(Options{Workspace: newWorkspace(t, t.TempDir())})
	if strings.Contains(got, "プロジェクトメモリ") {
		t.Error("CLAUDE.md がないのにメモリセクションが含まれている")
	}
}

// 段階的開示の検証。スキルは名前と説明だけを載せ、本文は載せない。
// ここが逆になると、スキルを増やすほど毎ターンのコストが膨らむ。
func TestBuildIncludesSkillIndexOnly(t *testing.T) {
	got := Build(Options{
		Workspace: newWorkspace(t, t.TempDir()),
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
	got := Build(Options{Workspace: newWorkspace(t, t.TempDir())})
	if strings.Contains(got, "利用可能なスキル") {
		t.Error("スキルがないのにスキルセクションが含まれている")
	}
}
