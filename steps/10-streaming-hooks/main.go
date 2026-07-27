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
//	go run ./steps/10-streaming-hooks   # 対話モード(REPL)
//	go run ./steps/10-streaming-hooks -p "質問"   # ワンショットモード
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/agent"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/command"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/config"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/hooks"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/mcp"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/permission"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/prompt"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/skill"
	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/tools"
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
	mcpConfig, err := config.LoadMCPConfig(workDir)
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

	// --- 設定ファイルの信頼確認 ---
	// 設定ファイルは読み込み済みだが、まだ何も実行していない。
	// MCPサーバーの起動もhookの実行もこの先なので、ここが最後の
	// 引き返せる地点である。
	if err := confirmTrust(workDir, config.Executables(settings, mcpConfig), makeAsker(stdin, "[y]es / [n]o")); err != nil {
		return err
	}

	// --- ツールの組み立て ---
	// 組み込みツール + スキル + MCPツール + Taskツール(サブエージェント)。
	baseTools := []tools.Tool{
		tools.NewReadFile(ws),
		tools.NewListFiles(ws),
		tools.NewEditFile(ws),
		tools.NewBash(ws),
	}

	// スキルもファイルを読む機能なので、封じ込め(ws)を通す。
	skills, err := skill.Load(ws, os.Stderr)
	if err != nil {
		return err
	}
	if len(skills) > 0 {
		baseTools = append(baseTools, skill.NewTool(skills))
	}

	mcpTools, closeMCP := connectMCPServers(ctx, mcpConfig)
	defer closeMCP()
	baseTools = append(baseTools, mcpTools...)

	// --- 権限の組み立て ---
	// read_file と list_files は読み取り専用なので無条件許可。
	// edit_file と bash(と未知のMCPツール)は承認フローに乗せる。
	newChecker := func(ask permission.Asker) *permission.Checker {
		return permission.New(permission.Config{
			ToolPolicies: map[string]permission.Policy{
				// read_file と list_files を無条件許可にできるのは、
				// Workspace で作業ディレクトリに封じ込めてあるからである。
				// 封じ込めがなければ「読み取りだから安全」は成り立たない
				// (~/.ssh/id_rsa も .env も読めてしまう)。
				"read_file":  permission.Allow,
				"list_files": permission.Allow,
				// スキルは .agent/skills 配下の手順書を読むだけ。
				"skill": permission.Allow,
				// サブエージェント起動自体は安全。サブエージェントが使う
				// 個々のツールは、そのつど同じ承認フローを通る。
				"task": permission.Allow,
			},
			BashAllowlist: settings.BashAllowlist,
			Ask:           ask,
		})
	}
	checker := newChecker(asker)

	// サブエージェント用には別の Checker を作る。ポリシーは同じだが、
	// 質問の出所を明示する点だけが違う。サブエージェントの出力は
	// 画面に出していない(下の Out: io.Discard)ので、承認だけが
	// 文脈なしに現れると、ユーザーは何に答えているのか分からない。
	//
	// 「人に聞けないから自動で拒否する」という選択肢もあるが、それだと
	// サブエージェントは調査しかできなくなる。誰が要求しているかを
	// 伝えたうえで人間に判断させる方を採った。
	subChecker := newChecker(func(question string) (string, error) {
		return asker("(サブエージェントからの要求) " + question)
	})

	client := llm.NewAnthropicClient()
	model := defaultModel
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		model = anthropic.Model(m)
	}
	// スキルは目次(名前+説明)だけをシステムプロンプトに載せる。
	// 本文は skill ツールが呼ばれたときに初めてコンテキストに入る。
	skillSummaries := make([]prompt.SkillSummary, 0, len(skills))
	for _, s := range skills {
		skillSummaries = append(skillSummaries, prompt.SkillSummary{Name: s.Name, Description: s.Description})
	}
	systemPrompt := prompt.Build(prompt.Options{WorkDir: workDir, Skills: skillSummaries})
	hookRunner := hooks.NewRunner(settings.Hooks)

	// --- サブエージェントの組み立て ---
	// 親と同じツール・hooksを持つが、
	//   - Taskツールは持たない(再帰の禁止)
	//   - 出力は捨てる(親の表示を汚さない)
	//   - 承認の質問に出所を付ける
	// という3点だけが違う。
	taskTool := agent.NewTaskTool(func() *agent.Agent {
		return agent.New(agent.Config{
			Client:       client,
			Model:        model,
			SystemPrompt: systemPrompt,
			Tools:        baseTools,
			Permission:   subChecker,
			Hooks:        hookRunner,
			Out:          io.Discard,
		})
	})

	mainAgent := agent.New(agent.Config{
		Client:       client,
		Model:        model,
		SystemPrompt: systemPrompt,
		// append(baseTools, taskTool) と書いてはいけない。baseTools に
		// 容量の余りがあると、追記先が上のクロージャが参照している
		// 配列そのものになりうる。今は実害がなくても、あとから
		// 要素を足したときに黙って壊れる類のバグなので、
		// 新しいスライスを作る slices.Concat を使う。
		Tools:      slices.Concat(baseTools, []tools.Tool{taskTool}),
		Permission: checker,
		Hooks:      hookRunner,
		Out:        os.Stdout,
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

// confirmTrust は「このディレクトリの設定ファイルを信頼してよいか」を
// 起動時に1度だけ確認する。
//
// 設定ファイルは起動時の実行権限そのものである。悪意あるリポジトリは
// .mcp.json に1行書いておくだけで、clone した人がエージェントを
// 起動した瞬間に任意のコマンドを走らせられる。中身を見せる機会を
// 作らずに実行するわけにはいかない。
//
// 拒否されたら起動を中止する。hooks を無効化して続行しない理由は、
// hooks が防御にも使われるからである(example の rm -rf ブロッカーが
// まさにそれ)。「設定を信頼しないから防御を外して動かす」は本末転倒で、
// 信頼できない設定のディレクトリでは動かさないのが唯一の安全側である。
//
// 実物のエージェントは、一度信頼したディレクトリをユーザー設定に
// 記録して2回目以降は聞かない。ここでは毎回聞く(永続化する場所を
// 増やすと、今度はその設定ファイルの信頼が問題になる)。
func confirmTrust(workDir string, executables []config.Executable, ask permission.Asker) error {
	if len(executables) == 0 {
		return nil // 実行されるものが何もないなら聞くことはない
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s の設定ファイルは、次の外部コマンド実行を要求しています:\n", workDir)
	for _, e := range executables {
		fmt.Fprintf(&b, "  - %s\n      %s\n", e.Source, e.Command)
	}
	b.WriteString("内容を確認しました。このディレクトリの設定を信頼して起動しますか?")

	answer, err := ask(b.String())
	if err != nil {
		// 標準入力が繋がっていない(CIなど)場合もここに来る。
		// 「聞けなかったから通す」は安全機構としてありえないので、
		// 答えが得られないこと自体を起動の失敗として扱う。
		return fmt.Errorf("設定を信頼するか確認できませんでした: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return errors.New("設定を信頼しなかったため起動を中止しました")
	}
}

// connectMCPServers は .mcp.json に定義された全サーバーに接続し、
// ツール一覧を集める。1つのサーバーへの接続失敗は警告に留め、
// エージェント全体は起動させる。
func connectMCPServers(ctx context.Context, cfg config.MCPConfig) ([]tools.Tool, func()) {
	var allTools []tools.Tool
	var clients []*mcp.Client
	for _, name := range slices.Sorted(maps.Keys(cfg.MCPServers)) {
		server := cfg.MCPServers[name]
		client, err := mcp.Connect(ctx, mcp.Server{
			Name:    name,
			Command: server.Command,
			Args:    server.Args,
			// サーバーのログは捨てずに標準エラー出力へ流す。
			// MCPサーバーが動かないときの手がかりはここにしかない。
			Stderr: os.Stderr,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "警告: %v\n", err)
			continue
		}
		serverTools, err := client.Tools(ctx)
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
	return allTools, closeAll
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
