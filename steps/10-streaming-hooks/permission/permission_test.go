package permission

import (
	"encoding/json"
	"testing"
)

// fixedAsker は常に同じ回答を返す Asker。質問された回数も数える。
func fixedAsker(answer string, count *int) Asker {
	return func(question string) (string, error) {
		*count++
		return answer, nil
	}
}

func TestAllowPolicyDoesNotAsk(t *testing.T) {
	asked := 0
	c := New(Config{
		ToolPolicies: map[string]Policy{"read_file": Allow},
		Ask:          fixedAsker("n", &asked),
	})
	if err := c.Check("read_file", json.RawMessage(`{"path":"a"}`)); err != nil {
		t.Fatalf("Allowポリシーのツールが拒否された: %v", err)
	}
	if asked != 0 {
		t.Errorf("Allowポリシーなのに確認された(%d回)", asked)
	}
}

func TestUnknownToolAsksByDefault(t *testing.T) {
	asked := 0
	c := New(Config{Ask: fixedAsker("y", &asked)})
	if err := c.Check("mcp__foo__bar", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("yと答えたのに拒否された: %v", err)
	}
	if asked != 1 {
		t.Errorf("未登録ツールは確認されるべき(実際: %d回)", asked)
	}
}

func TestDenyByUser(t *testing.T) {
	asked := 0
	c := New(Config{Ask: fixedAsker("n", &asked)})
	err := c.Check("edit_file", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("nと答えたのに許可された")
	}
}

func TestBashAllowlist(t *testing.T) {
	asked := 0
	c := New(Config{
		BashAllowlist: []string{"ls", "go"},
		Ask:           fixedAsker("n", &asked),
	})

	// allowlistにあるコマンドは確認なしで通る。
	if err := c.Check("bash", json.RawMessage(`{"command":"go test ./..."}`)); err != nil {
		t.Fatalf("allowlist内のコマンドが拒否された: %v", err)
	}
	if asked != 0 {
		t.Error("allowlist内のコマンドなのに確認された")
	}

	// allowlistにないコマンドは確認され、nなら拒否。
	if err := c.Check("bash", json.RawMessage(`{"command":"rm -rf /"}`)); err == nil {
		t.Fatal("allowlist外のコマンドが確認なしで通った")
	}
	if asked != 1 {
		t.Errorf("allowlist外のコマンドは確認されるべき(実際: %d回)", asked)
	}
}

// allowlistは「先頭単語が一致すれば許可」なので、1コマンドに見せかけて
// 2つ目のコマンドを混ぜられると素通りしてしまう。ここはその全パターンを
// 塞げているかの回帰テスト。1つでも漏れるとallowlistは意味を失う。
func TestBashAllowlistCannotBeBypassed(t *testing.T) {
	bypasses := map[string]string{
		"セミコロン":    "ls; rm -rf /",
		"改行":       "ls\nrm -rf /",
		"パイプ":      "ls | sh",
		"AND":      "ls && rm -rf /",
		"コマンド置換":   "ls $(rm -rf /)",
		"バックティック":  "ls `rm -rf /`",
		"リダイレクト":   "echo pwned > /etc/hosts",
		"追記":       "echo pwned >> ~/.bashrc",
		"入力":       "cat < /etc/passwd",
		"サブシェル":    "ls (rm -rf /)",
		"変数展開":     "ls $HOME/../../etc",
		"クォート":     `ls "; rm -rf /"`,
		"バックスラッシュ": `ls \; rm -rf /`,
	}

	for name, command := range bypasses {
		t.Run(name, func(t *testing.T) {
			asked := 0
			c := New(Config{
				BashAllowlist: []string{"ls", "cat", "echo"},
				Ask:           fixedAsker("n", &asked),
			})
			input, _ := json.Marshal(map[string]string{"command": command})
			if err := c.Check("bash", input); err == nil {
				t.Errorf("allowlistを素通りした: %s", command)
			}
			if asked != 1 {
				t.Errorf("確認されるべき(実際: %d回): %s", asked, command)
			}
		})
	}
}

// 逆方向の確認。単純なコマンドは確認なしで通らないと、
// 確認疲れで「なんでもy」の事故につながる。
func TestBashAllowlistStillAllowsSimpleCommands(t *testing.T) {
	simple := []string{
		"ls",
		"ls -la",
		"cat README.md",
		"go test ./...",
		"go build -o bin/app ./cmd/app",
		"grep -rn foo internal/",
	}
	for _, command := range simple {
		asked := 0
		c := New(Config{
			BashAllowlist: []string{"ls", "cat", "go", "grep"},
			Ask:           fixedAsker("n", &asked),
		})
		input, _ := json.Marshal(map[string]string{"command": command})
		if err := c.Check("bash", input); err != nil {
			t.Errorf("単純なコマンドが拒否された: %s (%v)", command, err)
		}
		if asked != 0 {
			t.Errorf("単純なコマンドで確認された: %s", command)
		}
	}
}

func TestAlwaysAllowRemembersInSession(t *testing.T) {
	asked := 0
	c := New(Config{Ask: fixedAsker("a", &asked)})

	if err := c.Check("bash", json.RawMessage(`{"command":"make build"}`)); err != nil {
		t.Fatalf("aと答えたのに拒否された: %v", err)
	}
	// 2回目は確認なしで通る。
	if err := c.Check("bash", json.RawMessage(`{"command":"make test"}`)); err != nil {
		t.Fatalf("2回目のmakeが拒否された: %v", err)
	}
	if asked != 1 {
		t.Errorf("aの後は確認をスキップすべき(実際: %d回)", asked)
	}
}

func TestNoAskerDeniesSafely(t *testing.T) {
	// Askerがない文脈(サブエージェント等)では、確認が必要な操作は拒否される。
	c := New(Config{})
	if err := c.Check("edit_file", json.RawMessage(`{}`)); err == nil {
		t.Fatal("Askerなしで承認必須ツールが通った")
	}
}
