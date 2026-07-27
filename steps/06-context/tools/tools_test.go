package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, tool Tool, input string) (string, error) {
	t.Helper()
	return tool.Run(context.Background(), json.RawMessage(input))
}

func testCtx() context.Context { return context.Background() }

// jsonObject はキーと値の並びからJSONオブジェクトを作るテストヘルパー。
func jsonObject(kv ...string) (json.RawMessage, error) {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return json.Marshal(m)
}

// workspaceFor は dir を作業ディレクトリとする Workspace を作る。
func workspaceFor(t *testing.T, dir string) *Workspace {
	t.Helper()
	ws, err := NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return ws
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello, world"), 0o644))

	got, err := run(t, NewReadFile(workspaceFor(t, dir)), `{"path":"hello.txt"}`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "hello, world" {
		t.Errorf("got %q, want %q", got, "hello, world")
	}
}

func TestReadFileNotFound(t *testing.T) {
	ws := workspaceFor(t, t.TempDir())
	if _, err := run(t, NewReadFile(ws), `{"path":"no/such/file.txt"}`); err == nil {
		t.Fatal("存在しないファイルはエラーになるべき")
	}
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	must(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755)) // 隠しディレクトリは除外される
	must(t, os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, ".git", "config"), nil, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "sub", "b.go"), nil, 0o644))

	got, err := run(t, NewListFiles(workspaceFor(t, dir)), `{"path":"."}`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"a.go", "sub/", filepath.Join("sub", "b.go")} {
		if !strings.Contains(got, want) {
			t.Errorf("出力に %q が含まれていない:\n%s", want, got)
		}
	}
	if strings.Contains(got, ".git") {
		t.Errorf("隠しディレクトリが含まれている:\n%s", got)
	}
}

func TestEditFileReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	must(t, os.WriteFile(path, []byte("func old() {}\n"), 0o644))

	input, _ := jsonObject("path", "main.go", "old_str", "func old()", "new_str", "func new()")
	if _, err := NewEditFile(workspaceFor(t, dir)).Run(testCtx(), input); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, _ := os.ReadFile(path)
	if got := string(data); got != "func new() {}\n" {
		t.Errorf("got %q", got)
	}
}

func TestEditFileCreate(t *testing.T) {
	dir := t.TempDir()

	input, _ := jsonObject("path", "nested/new.txt", "old_str", "", "new_str", "created")
	if _, err := NewEditFile(workspaceFor(t, dir)).Run(testCtx(), input); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "nested", "new.txt"))
	if got := string(data); got != "created" {
		t.Errorf("got %q", got)
	}
}

func TestEditFileRejectsAmbiguousMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	must(t, os.WriteFile(path, []byte("x\nx\n"), 0o644))

	input, _ := jsonObject("path", "f.txt", "old_str", "x", "new_str", "y")
	if _, err := NewEditFile(workspaceFor(t, dir)).Run(testCtx(), input); err == nil {
		t.Fatal("old_str が複数マッチする編集はエラーになるべき")
	}
	// 失敗した編集はファイルを変更しない。
	data, _ := os.ReadFile(path)
	if got := string(data); got != "x\nx\n" {
		t.Errorf("失敗した編集でファイルが変わった: %q", got)
	}
}

func TestEditFileRejectsNoMatch(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("abc"), 0o644))

	input, _ := jsonObject("path", "f.txt", "old_str", "zzz", "new_str", "y")
	if _, err := NewEditFile(workspaceFor(t, dir)).Run(testCtx(), input); err == nil {
		t.Fatal("old_str がマッチしない編集はエラーになるべき")
	}
}

func TestBash(t *testing.T) {
	ws := workspaceFor(t, t.TempDir())
	got, err := run(t, NewBash(ws), `{"command":"echo hello"}`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(got) != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestBashFailureIncludesOutput(t *testing.T) {
	ws := workspaceFor(t, t.TempDir())
	_, err := run(t, NewBash(ws), `{"command":"echo oops >&2; exit 3"}`)
	if err == nil {
		t.Fatal("非ゼロ終了はエラーになるべき")
	}
	// エラーメッセージに出力が含まれること。LLMは出力を見て原因を推測する。
	if !strings.Contains(err.Error(), "oops") {
		t.Errorf("エラーにコマンド出力が含まれていない: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
