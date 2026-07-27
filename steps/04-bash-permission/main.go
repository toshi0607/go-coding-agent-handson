// go-coding-agent-handson のミニコーディングエージェント(step 04 時点)。
//
// ここ(main)の仕事は「組み立て」である。各パッケージが提供する部品
// (LLMクライアント、ツール、権限、コンテキスト管理)を配線して
// エージェントを作り、REPLに渡す。どの部品に何を渡しているかを追うと、
// エージェント全体の構造が見える。
//
// 使い方:
//
//	export ANTHROPIC_API_KEY=...
//	go run ./steps/04-bash-permission   # 対話モード(REPL)
//	go run ./steps/04-bash-permission -p "質問"   # ワンショットモード
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/04-bash-permission/agent"
	"github.com/toshi0607/go-coding-agent-handson/steps/04-bash-permission/config"
	"github.com/toshi0607/go-coding-agent-handson/steps/04-bash-permission/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/04-bash-permission/permission"
	"github.com/toshi0607/go-coding-agent-handson/steps/04-bash-permission/tools"
)

// defaultModel は使用するモデル。環境変数 ANTHROPIC_MODEL で上書きできる。
const defaultModel = anthropic.Model("claude-opus-5")

func main() {
	oneShot := flag.String("p", "", "対話モードの代わりに、この1つの依頼を実行して終了する")
	flag.Parse()

	if err := run(*oneShot); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
}

func run(oneShot string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	workDir, err := os.Getwd()
	if err != nil {
		return err
	}

	settings, err := config.LoadSettings(workDir)
	if err != nil {
		return err
	}

	stdin := bufio.NewReader(os.Stdin)
	asker := makeAsker(stdin, "[y]es / [n]o / [a]lways")

	// --- ファイル操作の封じ込め ---
	// ファイル系ツールはこの Workspace 経由でしかパスを解決できない。
	// LLMが渡してきたパスを直接OSに投げないための境界である。
	ws, err := tools.NewWorkspace(workDir)
	if err != nil {
		return err
	}
	// 以降は解決済みの絶対パス(ws.Root())を使う。os.Getwd() の値を
	// そのまま使い回すと、macOSで /tmp が /private/tmp のリンクである
	// ために「システムプロンプトに書かれた作業ディレクトリ」と
	// 「実際の封じ込め境界」が食い違う。
	workDir = ws.Root()

	// --- ツールの組み立て ---
	baseTools := []tools.Tool{
		tools.NewReadFile(ws),
		tools.NewListFiles(ws),
		tools.NewEditFile(ws),
		tools.NewBash(ws),
	}

	// --- 権限の組み立て ---
	// read_file と list_files は読み取り専用なので無条件許可。
	// edit_file と bash は承認フローに乗せる。
	checker := permission.New(permission.Config{
		ToolPolicies: map[string]permission.Policy{
			// read_file と list_files を無条件許可にできるのは、
			// Workspace で作業ディレクトリに封じ込めてあるからである。
			// 封じ込めがなければ「読み取りだから安全」は成り立たない
			// (~/.ssh/id_rsa も .env も読めてしまう)。
			"read_file":  permission.Allow,
			"list_files": permission.Allow,
		},
		BashAllowlist: settings.BashAllowlist,
		Ask:           asker,
	})

	client := llm.NewAnthropicClient()
	model := defaultModel
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		model = anthropic.Model(m)
	}

	mainAgent := agent.New(agent.Config{
		Client:     client,
		Model:      model,
		Tools:      baseTools,
		Permission: checker,
		Out:        os.Stdout,
	})

	if oneShot != "" {
		_, err := mainAgent.Run(ctx, oneShot)
		return err
	}
	return repl(ctx, mainAgent, stdin)
}

// repl は対話ループ。入力を読み、エージェントに渡す。
func repl(ctx context.Context, a *agent.Agent, stdin *bufio.Reader) error {
	fmt.Println("ミニコーディングエージェント(Ctrl-D で終了)")
	for {
		fmt.Print("\n> ")
		line, err := stdin.ReadString('\n')
		if err == io.EOF {
			fmt.Println()
			return nil
		}
		if err != nil {
			return err
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if _, err := a.Run(ctx, input); err != nil {
			if ctx.Err() != nil {
				return nil // Ctrl-C
			}
			fmt.Println("エラー:", err)
		}
	}
}

// makeAsker は標準入力から承認を得る Asker を作る。
// choices は選択肢の表示("[y]es / [n]o" など)。
func makeAsker(stdin *bufio.Reader, choices string) permission.Asker {
	return func(question string) (string, error) {
		fmt.Printf("\n%s\n  %s > ", question, choices)
		answer, err := stdin.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(answer), nil
	}
}
