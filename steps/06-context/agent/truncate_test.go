package agent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateToolResult(t *testing.T) {
	if got := truncateToolResult("short", 10); got != "short" {
		t.Errorf("短い結果は切り詰めない: %q", got)
	}

	long := strings.Repeat("a", 100)
	got := truncateToolResult(long, 10)
	if !strings.HasPrefix(got, strings.Repeat("a", 10)) {
		t.Errorf("先頭が保存されていない: %q", got)
	}
	// 切り詰めた事実がLLMに伝わることが重要。
	if !strings.Contains(got, "切り詰め") {
		t.Errorf("切り詰めの報告がない: %q", got)
	}
}

// バイト単位で切ると日本語がちょうど切れ目で壊れる。
// 「10文字で切り詰めた」と報告しながら壊れたバイト列を送るのは、
// LLMにとってもユーザーにとっても嘘になる。
func TestTruncateToolResultKeepsUTF8Intact(t *testing.T) {
	// 1文字3バイトの日本語30文字 = 90バイト。
	long := strings.Repeat("あ", 30)
	got := truncateToolResult(long, 10)

	if !strings.HasPrefix(got, strings.Repeat("あ", 10)) {
		t.Errorf("先頭10文字が保存されていない: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("UTF-8が壊れている: %q", got)
	}
	// 報告に含める「全体の長さ」も文字数であること。
	// バイト数で数えると 90 になる(1文字3バイト × 30文字)。
	if !strings.Contains(got, "30") || strings.Contains(got, "90") {
		t.Errorf("全体の文字数(30)がバイト数(90)になっている: %q", got)
	}

	// ちょうど上限ぴったりのときは切り詰めない。
	exact := strings.Repeat("あ", 10)
	if got := truncateToolResult(exact, 10); got != exact {
		t.Errorf("上限ぴったりは切り詰めないはず: %q", got)
	}
}
