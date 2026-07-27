package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreToolUseBlocks(t *testing.T) {
	// 終了コード2でブロック。stderrの内容が理由としてLLMに返る。
	r := NewRunner(Config{
		"PreToolUse": {{Matcher: "bash", Command: `echo "rmは禁止です" >&2; exit 2`}},
	})
	err := r.RunPre(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /"}`))
	if err == nil {
		t.Fatal("exit 2 のフックはツール実行をブロックすべき")
	}
	if !strings.Contains(err.Error(), "rmは禁止です") {
		t.Errorf("stderrの内容がエラーに含まれていない: %v", err)
	}
}

func TestPreToolUseAllows(t *testing.T) {
	r := NewRunner(Config{
		"PreToolUse": {{Matcher: "bash", Command: "exit 0"}},
	})
	if err := r.RunPre(context.Background(), "bash", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("exit 0 のフックはブロックしないはず: %v", err)
	}
}

// フックの契約は exit 0(許可)と exit 2(ブロック)だけ。
// それ以外は「検査が完了していない」ので安全側に倒してブロックする。
// タイムアウトやスクリプトのバグで防御が黙って無効化されるのが最悪。
func TestPreToolUseFailsClosed(t *testing.T) {
	cases := map[string]string{
		"シグナルで死ぬ":        `echo "検査中に死にました" >&2; kill -9 $$`,
		"コマンドが存在しない":     "this-command-does-not-exist-12345",
		"スクリプトのバグでexit1": "exit 1",
	}
	for name, command := range cases {
		t.Run(name, func(t *testing.T) {
			r := NewRunner(Config{"PreToolUse": {{Matcher: "*", Command: command}}})
			if err := r.RunPre(context.Background(), "bash", json.RawMessage(`{}`)); err == nil {
				t.Error("フックが正常に判定できなかった場合はブロックすべき")
			}
		})
	}
}

func TestMatcherFiltersByToolName(t *testing.T) {
	// bash向けのフックは read_file には発火しない。
	r := NewRunner(Config{
		"PreToolUse": {{Matcher: "bash", Command: "exit 2"}},
	})
	if err := r.RunPre(context.Background(), "read_file", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("matcher対象外のツールでフックが発火した: %v", err)
	}
}

func TestHookReceivesPayloadOnStdin(t *testing.T) {
	// フックには実行内容がJSONで渡される。jqなどで検査できることの確認。
	dir := t.TempDir()
	outFile := filepath.Join(dir, "payload.json")
	r := NewRunner(Config{
		"PostToolUse": {{Matcher: "*", Command: "cat > " + outFile}},
	})
	r.RunPost(context.Background(), "bash", json.RawMessage(`{"command":"ls"}`), "file1\nfile2")

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("フックが実行されていない: %v", err)
	}
	var payload struct {
		HookEventName string          `json:"hook_event_name"`
		ToolName      string          `json:"tool_name"`
		ToolInput     json.RawMessage `json:"tool_input"`
		ToolOutput    string          `json:"tool_output"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("ペイロードがJSONではない: %v", err)
	}
	if payload.HookEventName != "PostToolUse" || payload.ToolName != "bash" || payload.ToolOutput != "file1\nfile2" {
		t.Errorf("ペイロードの内容が不正: %+v", payload)
	}
}
