// Package permission はツール実行の承認フローを実装する。
//
// エージェントの安全性は「LLMを信用しない」ことから始まる。
// LLMの出力するツール呼び出しは提案にすぎず、実行するかどうかの
// 最終判断は必ずこちら側(プログラムとユーザー)が持つ。
//
// ツールは危険度に応じて3段階に分ける:
//   - Allow: 読み取り専用など、無条件で実行してよいもの
//   - Ask:   ファイル編集など、ユーザーの承認が要るもの
//   - Deny:  常に拒否するもの
//
// bash だけは特別扱いで、「コマンドの先頭単語が allowlist にあれば Allow、
// なければ Ask」という2段構えにする。go test や ls のような定番コマンドを
// 毎回確認するのは煩わしく、確認疲れは「なんでも y を押す」事故につながる。
// 安全性とUXはトレードオフではなく、UXの悪い安全機構は安全ですらない。
package permission

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Policy はツールに対する実行ポリシー。
type Policy int

const (
	// Ask はユーザーに確認する(未登録ツールのデフォルト。安全側に倒す)。
	Ask Policy = iota
	// Allow は無条件で実行を許可する。
	Allow
	// Deny は無条件で拒否する。
	Deny
)

// Asker はユーザーに質問して回答を得る関数。
// 返り値は "y"(今回のみ許可) / "a"(以後ずっと許可) / それ以外(拒否)。
type Asker func(question string) (string, error)

// Checker はツール実行の可否を判定する。
type Checker struct {
	policies      map[string]Policy
	bashAllowlist map[string]bool
	ask           Asker
	// sessionAllowed は「a」で承認済みのキー(ツール名 or bashコマンド名)。
	// このセッション中だけ有効で、永続化はしない。
	sessionAllowed map[string]bool
}

// Config は Checker の設定。
type Config struct {
	// ToolPolicies はツール名 → ポリシー。未登録のツールは Ask になる。
	ToolPolicies map[string]Policy
	// BashAllowlist は承認なしで実行してよいbashコマンド名(先頭単語)。
	BashAllowlist []string
	// Ask はユーザーへの確認方法。nil の場合、確認が必要な操作はすべて
	// 拒否される(サブエージェントなど、人に聞けない文脈で使う)。
	Ask Asker
}

// New は Checker を作る。
func New(cfg Config) *Checker {
	allow := make(map[string]bool, len(cfg.BashAllowlist))
	for _, cmd := range cfg.BashAllowlist {
		allow[cmd] = true
	}
	policies := cfg.ToolPolicies
	if policies == nil {
		policies = map[string]Policy{}
	}
	return &Checker{
		policies:       policies,
		bashAllowlist:  allow,
		ask:            cfg.Ask,
		sessionAllowed: map[string]bool{},
	}
}

// Check はツール実行の可否を判定する。実行してよければ nil、
// 拒否なら理由入りの error を返す(エージェントはこれを is_error 付きの
// tool_result としてLLMに返す)。
func (c *Checker) Check(toolName string, input json.RawMessage) error {
	// bash はコマンド単位で判定する。
	if toolName == "bash" {
		return c.checkBash(input)
	}

	switch c.policies[toolName] { // 未登録はゼロ値 = Ask
	case Allow:
		return nil
	case Deny:
		return fmt.Errorf("ツール %s は設定で禁止されています", toolName)
	default:
		if c.sessionAllowed[toolName] {
			return nil
		}
		question := fmt.Sprintf("ツール %s の実行を許可しますか?\n  入力: %s", toolName, summarize(input))
		return c.confirm(question, toolName, "操作")
	}
}

func (c *Checker) checkBash(input json.RawMessage) error {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return fmt.Errorf("bashの入力をパースできません: %w", err)
	}
	head := commandHead(in.Command)

	// 単純なコマンドでなければ allowlist を適用しない。
	//
	// ここは allowlist 方式の最大の落とし穴である。先頭単語だけ見て
	// 許可すると、`ls; rm -rf /` も `ls\nrm -rf /` も `echo x > ~/.ssh/authorized_keys`
	// も「先頭は ls / echo だから安全」と誤判定してしまう。
	// シェルの構文は区切り文字・改行・リダイレクト・置換と多様で、
	// 「危険な形を列挙して弾く」(ブラックリスト)は必ず漏れる。
	//
	// そこで判定を反転し、「安全と言い切れる形だけを通す」
	// (ホワイトリスト)にする。1コマンド・1行・シェルのメタ文字なし。
	// それ以外は先頭単語が何であれユーザー確認に回す。
	if !isSimpleCommand(in.Command) {
		question := fmt.Sprintf("シェルのメタ文字を含むコマンドの実行を許可しますか?\n  $ %s", in.Command)
		return c.confirm(question, "", "コマンド")
	}

	if c.bashAllowlist[head] || c.sessionAllowed["bash:"+head] {
		return nil
	}
	question := fmt.Sprintf("コマンドの実行を許可しますか?\n  $ %s", in.Command)
	return c.confirm(question, "bash:"+head, "コマンド")
}

// confirm はユーザーに確認する。sessionKey が空でなく回答が "a" なら
// セッション中は同じ確認をスキップする。
func (c *Checker) confirm(question, sessionKey, kind string) error {
	if c.ask == nil {
		return fmt.Errorf("この%sには承認が必要ですが、承認できる場面ではないため拒否しました", kind)
	}
	answer, err := c.ask(question)
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	case "a", "always":
		if sessionKey != "" {
			c.sessionAllowed[sessionKey] = true
		}
		return nil
	default:
		// 拒否の理由をLLMに伝えることで、LLMは別のアプローチを検討できる。
		return fmt.Errorf("ユーザーがこの%sを拒否しました。別の方法を検討するか、ユーザーに意図を確認してください", kind)
	}
}

// isSimpleCommand はコマンドが「1つのコマンド呼び出しだけ」であることを
// 保証できる形かを判定する。許可するのは英数字と、パス・オプションに
// 現れるごく限られた記号のみ。改行・リダイレクト・パイプ・変数展開・
// クォート・ワイルドカードはすべて「単純でない」と判定する。
func isSimpleCommand(command string) bool {
	for _, r := range command {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ', r == '-', r == '_', r == '.', r == '/', r == '=', r == ':', r == ',', r == '+', r == '@':
		default:
			// マルチバイト文字(日本語のファイル名など)もここに落ちる。
			// 安全側に倒して確認に回す。
			return false
		}
	}
	return true
}

// commandHead はコマンド文字列の先頭単語を返す。
func commandHead(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// summarize は確認プロンプト表示用に入力JSONを整形する。
func summarize(input json.RawMessage) string {
	s := string(input)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
