// go-coding-agent-handson の完成形ミニコーディングエージェント。
//
// ここ(main)の仕事は「組み立て」である。各パッケージが提供する部品
// (LLMクライアント、ツール、権限、hooks、コンテキスト管理)を配線して
// エージェントを作り、REPLに渡す。どの部品に何を渡しているかを追うと、
// エージェント全体の構造が見える。
//
// 使い方:
//
//	export ANTHROPIC_API_KEY=...
//	go run ./final            # 対話モード(REPL)
//	go run ./final -p "質問"   # ワンショットモード
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
	"github.com/toshi0607/go-coding-agent-handson/final/agent"
	"github.com/toshi0607/go-coding-agent-handson/final/command"
	"github.com/toshi0607/go-coding-agent-handson/final/config"
	"github.com/toshi0607/go-coding-agent-handson/final/hooks"
	"github.com/toshi0607/go-coding-agent-handson/final/llm"
	"github.com/toshi0607/go-coding-agent-handson/final/mcp"
	"github.com/toshi0607/go-coding-agent-handson/final/permission"
	"github.com/toshi0607/go-coding-agent-handson/final/prompt"
	"github.com/toshi0607/go-coding-agent-handson/final/tools"
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

	// --- ツールの組み立て ---
	// 組み込みツール + MCPツール + Taskツール(サブエージェント)。
	baseTools := []tools.Tool{
		tools.ReadFile{},
		tools.ListFiles{},
		tools.EditFile{},
		tools.Bash{},
	}
	mcpTools, closeMCP, err := connectMCPServers(ctx, workDir)
	if err != nil {
		return err
	}
	defer closeMCP()
	baseTools = append(baseTools, mcpTools...)

	// --- 権限の組み立て ---
	// read_file と list_files は読み取り専用なので無条件許可。
	// edit_file と bash(と未知のMCPツール)は承認フローに乗せる。
	newChecker := func(ask permission.Asker) *permission.Checker {
		return permission.New(permission.Config{
			ToolPolicies: map[string]permission.Policy{
				"read_file":  permission.Allow,
				"list_files": permission.Allow,
				"task":       permission.Allow, // サブエージェント起動自体は安全。個々のツール実行時に改めてチェックされる
			},
			BashAllowlist: settings.BashAllowlist,
			Ask:           ask,
		})
	}
	checker := newChecker(makeAsker(stdin))

	client := llm.NewAnthropicClient()
	model := defaultModel
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		model = anthropic.Model(m)
	}
	systemPrompt := prompt.Build(workDir)
	hookRunner := hooks.NewRunner(settings.Hooks)

	// --- サブエージェントの組み立て ---
	// 親と同じツール・権限・hooksを持つが、
	//   - Taskツールは持たない(再帰の禁止)
	//   - 出力は捨てる(親の表示を汚さない)
	// という2点だけが違う。
	taskTool := agent.NewTaskTool(func() *agent.Agent {
		return agent.New(agent.Config{
			Client:       client,
			Model:        model,
			SystemPrompt: systemPrompt,
			Tools:        baseTools,
			Permission:   checker,
			Hooks:        hookRunner,
			Out:          io.Discard,
		})
	})

	mainAgent := agent.New(agent.Config{
		Client:       client,
		Model:        model,
		SystemPrompt: systemPrompt,
		Tools:        append(baseTools, taskTool),
		Permission:   checker,
		Hooks:        hookRunner,
		Out:          os.Stdout,
	})

	if oneShot != "" {
		_, err := mainAgent.Run(ctx, oneShot)
		return err
	}
	return repl(ctx, mainAgent, command.New(workDir), stdin)
}

// repl は対話ループ。入力を読み、スラッシュコマンドならここで処理し、
// それ以外はエージェントに渡す。
func repl(ctx context.Context, a *agent.Agent, commands *command.Registry, stdin *bufio.Reader) error {
	fmt.Println("ミニコーディングエージェント(/help でコマンド一覧、Ctrl-D で終了)")
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

		if commands.IsCommand(input) {
			result := commands.Execute(input)
			if result.Clear {
				a.ClearHistory()
			}
			if result.Compact {
				if err := a.CompactHistory(ctx); err != nil {
					fmt.Println("エラー:", err)
					continue
				}
				fmt.Println("コンテキストを圧縮しました。")
			}
			if result.Output != "" {
				fmt.Println(result.Output)
			}
			if result.Prompt == "" {
				continue
			}
			// カスタムコマンドの展開結果を通常の入力として扱う。
			input = result.Prompt
		}

		if _, err := a.Run(ctx, input); err != nil {
			if ctx.Err() != nil {
				return nil // Ctrl-C
			}
			fmt.Println("エラー:", err)
		}
	}
}

// connectMCPServers は .mcp.json に定義された全サーバーに接続し、
// ツール一覧を集める。1つのサーバーへの接続失敗は警告に留め、
// エージェント全体は起動させる。
func connectMCPServers(ctx context.Context, workDir string) ([]tools.Tool, func(), error) {
	cfg, err := config.LoadMCPConfig(workDir)
	if err != nil {
		return nil, nil, err
	}

	var allTools []tools.Tool
	var clients []*mcp.Client
	for name, server := range cfg.MCPServers {
		client, err := mcp.Connect(ctx, name, server.Command, server.Args...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告: %v\n", err)
			continue
		}
		serverTools, err := client.Tools()
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告: MCPサーバー %s のツール取得に失敗: %v\n", name, err)
			_ = client.Close()
			continue
		}
		clients = append(clients, client)
		allTools = append(allTools, serverTools...)
	}

	closeAll := func() {
		for _, c := range clients {
			_ = c.Close()
		}
	}
	return allTools, closeAll, nil
}

// makeAsker は標準入力から承認を得る Asker を作る。
func makeAsker(stdin *bufio.Reader) permission.Asker {
	return func(question string) (string, error) {
		fmt.Printf("\n%s\n  [y]es / [n]o / [a]lways > ", question)
		answer, err := stdin.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(answer), nil
	}
}
