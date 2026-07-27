package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestWorkspace(t *testing.T) (*Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return ws, dir
}

func TestResolveAllowsPathsInsideWorkspace(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	must(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "src", "a.go"), nil, 0o644))

	inside := []string{
		"src/a.go",
		"./src/a.go",
		"src/../src/a.go", // 一度上がっても中に戻れば正当
		".",
		"newfile.txt",         // まだ存在しないパスも解決できる
		"nested/new/file.txt", // 親ディレクトリも存在しない
	}
	for _, path := range inside {
		got, err := ws.Resolve(path)
		if err != nil {
			t.Errorf("作業ディレクトリ内のパスが拒否された: %s (%v)", path, err)
			continue
		}
		if !strings.HasPrefix(got, ws.Root()) {
			t.Errorf("解決結果が作業ディレクトリ外: %s → %s", path, got)
		}
	}
}

// 封じ込めの本体。ここが1つでも漏れると、read_file 経由で
// ~/.ssh/id_rsa や .env が読めてしまう。
func TestResolveRejectsEscapes(t *testing.T) {
	ws, _ := newTestWorkspace(t)

	escapes := map[string]string{
		"相対パスで上昇":     "../outside.txt",
		"相対パスで深く上昇":   "../../../../etc/passwd",
		"途中で上昇":       "src/../../outside.txt",
		"絶対パスで直接指定":   "/etc/passwd",
		"ホームディレクトリ":   "/Users",
		"末尾スラッシュ付き上昇": "../",
	}
	for name, path := range escapes {
		t.Run(name, func(t *testing.T) {
			if got, err := ws.Resolve(path); err == nil {
				t.Errorf("作業ディレクトリ外のパスが通った: %s → %s", path, got)
			}
		})
	}
}

// シンボリックリンク経由の脱出。文字列処理だけの検証では
// 絶対に防げないケースなので、独立したテストにしておく。
func TestResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windowsではシンボリックリンクの作成に特権が必要")
	}
	ws, dir := newTestWorkspace(t)

	// 作業ディレクトリの外に秘密のファイルを置く。
	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret.txt")
	must(t, os.WriteFile(secret, []byte("秘密"), 0o600))

	// 作業ディレクトリ内から外へのシンボリックリンクを張る。
	must(t, os.Symlink(outsideDir, filepath.Join(dir, "escape")))

	cases := map[string]string{
		"リンク先のファイル":  "escape/secret.txt",
		"リンクそのもの":    "escape",
		"リンク経由の新規作成": "escape/newfile.txt",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := ws.Resolve(path); err == nil {
				t.Errorf("シンボリックリンク経由で脱出できた: %s → %s", path, got)
			}
		})
	}
}

// 壊れたシンボリックリンク(リンクはあるがリンク先が存在しない)経由の脱出。
//
// EvalSymlinks は「存在しない」と「壊れたリンク」の両方で ENOENT を
// 返すため、両者を同一視すると「リンク自身のパス(=作業ディレクトリ内)」
// を返してしまい、os.WriteFile がリンクを辿って外に書き込む。
// gitは壊れたリンクをそのまま配布できるので、悪意あるリポジトリを
// cloneして中でエージェントを動かすだけで成立する。
func TestResolveRejectsDanglingSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windowsではシンボリックリンクの作成に特権が必要")
	}

	// リンクの張り方(絶対/相対)と深さを変えた3ケース。
	// target は作業ディレクトリを受け取って組み立てる——テスト側で
	// 相対パスを組み間違えると「作業ディレクトリ内に戻ってしまい、
	// 脱出していないのに脱出したと誤判定する」ので、必ず
	// filepath.Rel で作業ディレクトリからの相対パスを計算する。
	cases := map[string]struct {
		linkName string
		target   func(t *testing.T, workDir, victim string) string
		probe    string
	}{
		"絶対パスの壊れたリンク": {
			linkName: "notes.txt",
			target:   func(t *testing.T, workDir, victim string) string { return victim },
			probe:    "notes.txt",
		},
		"相対パスの壊れたリンク": {
			linkName: "notes.md",
			target: func(t *testing.T, workDir, victim string) string {
				rel, err := filepath.Rel(workDir, victim)
				if err != nil {
					t.Fatal(err)
				}
				return rel
			},
			probe: "notes.md",
		},
		"壊れたリンク配下": {
			linkName: "linkdir",
			target: func(t *testing.T, workDir, victim string) string {
				return filepath.Join(filepath.Dir(victim), "nodir")
			},
			probe: "linkdir/deep/file.txt",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			outsideDir := t.TempDir()
			victim := filepath.Join(outsideDir, "pwned.txt") // まだ存在しない

			target := tc.target(t, ws.Root(), victim)
			must(t, os.Symlink(target, filepath.Join(dir, tc.linkName)))

			if got, err := ws.Resolve(tc.probe); err == nil {
				t.Errorf("壊れたシンボリックリンク経由で脱出できた: %s (target=%s) → %s", tc.probe, target, got)
			}

			// ツール経由でも実際に外部へ書き込めないこと。
			input, _ := jsonObject("path", tc.probe, "old_str", "", "new_str", "PWNED")
			if _, err := NewEditFile(ws).Run(testCtx(), input); err == nil {
				t.Error("edit_file が壊れたリンク経由で書き込めた")
			}
			if _, err := os.Stat(victim); err == nil {
				t.Fatalf("作業ディレクトリ外の %s が作成された", victim)
			}
		})
	}
}

// 作業ディレクトリ内を指す壊れたリンクは正当なので通ること。
// 壊れたリンクを一律拒否すると誤検知になる。
func TestResolveAllowsDanglingSymlinkInsideWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windowsではシンボリックリンクの作成に特権が必要")
	}
	ws, dir := newTestWorkspace(t)
	must(t, os.Symlink(filepath.Join(dir, "notyet.txt"), filepath.Join(dir, "link.txt")))

	if _, err := ws.Resolve("link.txt"); err != nil {
		t.Errorf("作業ディレクトリ内を指す壊れたリンクが拒否された: %v", err)
	}
}

// 作業ディレクトリがファイルシステムのルートでも動くこと。
// root+"/" の前方一致で判定していると "//" になって内側を全部拒否する。
func TestWorkspaceAtFilesystemRoot(t *testing.T) {
	ws, err := NewWorkspace(string(filepath.Separator))
	if err != nil {
		t.Skipf("ルートを作業ディレクトリにできない環境: %v", err)
	}
	for _, path := range []string{"etc", "/etc"} {
		if _, err := ws.Resolve(path); err != nil {
			t.Errorf("ルート配下の %q が拒否された: %v", path, err)
		}
	}
}

// root と前方一致する別ディレクトリ("/x/app" と "/x/app-secrets")を
// 通してしまう典型的なバグの回帰テスト。
func TestResolveRejectsSiblingWithSharedPrefix(t *testing.T) {
	parent := t.TempDir()
	appDir := filepath.Join(parent, "app")
	secretDir := filepath.Join(parent, "app-secrets")
	must(t, os.MkdirAll(appDir, 0o755))
	must(t, os.MkdirAll(secretDir, 0o755))
	must(t, os.WriteFile(filepath.Join(secretDir, "key.txt"), []byte("秘密"), 0o600))

	ws, err := NewWorkspace(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ws.Resolve(filepath.Join(secretDir, "key.txt")); err == nil {
		t.Errorf("前方一致する別ディレクトリが通った: %s", got)
	}
}

// ツール経由でも封じ込めが効いていることの確認。
// Workspace 単体が正しくても、ツールが Resolve を呼び忘れていたら意味がない。
func TestToolsAreContained(t *testing.T) {
	ws, dir := newTestWorkspace(t)

	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret.txt")
	must(t, os.WriteFile(secret, []byte("秘密"), 0o600))

	t.Run("read_file", func(t *testing.T) {
		input, _ := jsonObject("path", secret)
		if _, err := NewReadFile(ws).Run(testCtx(), input); err == nil {
			t.Error("外部ファイルを読めてしまった")
		}
	})

	t.Run("list_files", func(t *testing.T) {
		input, _ := jsonObject("path", outsideDir)
		if _, err := NewListFiles(ws).Run(testCtx(), input); err == nil {
			t.Error("外部ディレクトリを一覧できてしまった")
		}
	})

	t.Run("edit_file", func(t *testing.T) {
		target := filepath.Join(outsideDir, "written.txt")
		input, _ := jsonObject("path", target, "old_str", "", "new_str", "書き込み")
		if _, err := NewEditFile(ws).Run(testCtx(), input); err == nil {
			t.Error("外部ファイルに書き込めてしまった")
		}
		if _, err := os.Stat(target); err == nil {
			t.Error("外部ファイルが実際に作成された")
		}
	})

	// 内側は普通に使えること(封じ込めが厳しすぎて使えないのも困る)。
	t.Run("内側は許可", func(t *testing.T) {
		must(t, os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("中身"), 0o644))
		input, _ := jsonObject("path", "ok.txt")
		got, err := NewReadFile(ws).Run(testCtx(), input)
		if err != nil {
			t.Fatalf("内側のファイルが読めない: %v", err)
		}
		if got != "中身" {
			t.Errorf("got %q", got)
		}
	})
}

// bash は作業ディレクトリで実行される。
// ただしパス封じ込めは効かない(コマンド文字列は検証できない)ため、
// bash の防御は承認フローが担う——という設計を明示するテスト。
func TestBashRunsInWorkspace(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	must(t, os.WriteFile(filepath.Join(dir, "marker.txt"), nil, 0o644))

	input, _ := jsonObject("command", "ls")
	got, err := NewBash(ws).Run(testCtx(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(got, "marker.txt") {
		t.Errorf("作業ディレクトリで実行されていない: %q", got)
	}
}
