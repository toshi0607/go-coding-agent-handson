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
	// 拒否される(テストやバッチ実行など、人に聞けない文脈で使う)。
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

	// TODO(step04-2): allowlist の判定を実装する。満たすべき仕様:
	//
	//  - コマンドが「単純な1コマンド」(isSimpleCommand)でなければ、
	//    allowlist を適用せず、必ずユーザー確認(c.confirm)に回す。
	//    先頭単語だけ見て許可すると `ls; rm -rf /` も「先頭は ls だから
	//    安全」になってしまう——なぜ判定の順番が命なのかは README を参照
	//  - 単純なコマンドで、先頭単語(commandHead)が allowlist にあるか、
	//    セッション中に "a" で許可済み(c.sessionAllowed["bash:"+先頭単語])
	//    なら、確認なしで許可する
	//  - それ以外は c.confirm で確認する。sessionKey は "bash:"+先頭単語
	//    (「a」で同じコマンドの再確認をスキップできるように)
	//
	// c.confirm(question, sessionKey, "コマンド") の question には
	// 実行しようとしているコマンド文字列そのものを含めること。
	// ユーザーは表示された内容だけを根拠に判断する。
	panic("TODO(step04-2): steps/04-bash-permission/permission/permission.go を実装してください(hints.md 参照)")
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
// 保証できる形かを判定する。
//
// TODO(step04-1): 実装する。設計方針:
//
// 「危険な文字(; | > など)を列挙して弾く」(ブラックリスト)を
// 考えたくなるが、その方向は必ず漏れる。シェルの構文は区切り文字・
// 改行・リダイレクト・コマンド置換・クォート・変数展開と多様で、
// 危険な形を全部列挙し切ることは現実的にできない。
//
// 判定を反転し、「安全と言い切れる文字だけを通す」(ホワイトリスト)に
// すること。通してよいのは英数字と、パスやオプションに現れるごく
// 限られた記号(スペース - _ . / = : , + @)だけ。それ以外の文字が
// 1つでも含まれていたら false(マルチバイト文字も安全側に倒す)。
//
// 検証テスト(TestBashAllowlistCannotBeBypassed)は13種類のバイパスを
// 試してくる。ブラックリストで書くと、どれかが必ずすり抜ける。
func isSimpleCommand(command string) bool {
	panic("TODO(step04-1): steps/04-bash-permission/permission/permission.go を実装してください(hints.md 参照)")
}

// commandHead はコマンド文字列の先頭単語を返す。
func commandHead(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// maxPromptRunes は確認プロンプトに表示する入力の文字数上限。
const maxPromptRunes = 200

// summarize は確認プロンプト表示用に入力JSONを整形する。
//
// バイト数ではなく文字数で切る。ユーザーが「何を許可するのか」を
// 読んで判断する画面なので、日本語のパスが文字化けするのは困る。
func summarize(input json.RawMessage) string {
	s := string(input)
	if r := []rune(s); len(r) > maxPromptRunes {
		s = string(r[:maxPromptRunes]) + "..."
	}
	return s
}
