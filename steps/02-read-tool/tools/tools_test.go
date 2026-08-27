package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func run(t *testing.T, tool Tool, input string) (string, error) {
	t.Helper()
	return tool.Run(t.Context(), json.RawMessage(input))
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello, world"), 0o644))

	got, err := run(t, NewReadFile(dir), `{"path":"hello.txt"}`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "hello, world" {
		t.Errorf("got %q, want %q", got, "hello, world")
	}
}

func TestReadFileSubdirectory(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("package a"), 0o644))

	got, err := run(t, NewReadFile(dir), `{"path":"src/a.go"}`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "package a" {
		t.Errorf("got %q", got)
	}
}

func TestReadFileNotFound(t *testing.T) {
	if _, err := run(t, NewReadFile(t.TempDir()), `{"path":"no/such/file.txt"}`); err == nil {
		t.Fatal("存在しないファイルはエラーになるべき")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
