// go-coding-agent-handson のミニコーディングエージェント(step 02 時点)。
//
// ここ(main)の仕事は「組み立て」である。各パッケージが提供する部品
// (LLMクライアント、ツール)を配線してエージェントを作り、REPLに渡す。
// どの部品に何を渡しているかを追うと、エージェント全体の構造が見える。
//
// 使い方:
//
//	export ANTHROPIC_API_KEY=...
//	go run ./steps/02-read-tool   # 対話モード(REPL)
//	go run ./steps/02-read-tool -p "質問"   # ワンショットモード
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
	"github.com/toshi0607/go-coding-agent-handson/steps/02-read-tool/agent"
	"github.com/toshi0607/go-coding-agent-handson/steps/02-read-tool/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/02-read-tool/tools"
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

	// --- ツールの組み立て ---
	baseTools := []tools.Tool{
		tools.NewReadFile(workDir),
	}

	client := llm.NewAnthropicClient()
	model := defaultModel
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		model = anthropic.Model(m)
	}

	mainAgent := agent.New(agent.Config{
		Client: client,
		Model:  model,
		Tools:  baseTools,
		Out:    os.Stdout,
	})

	if oneShot != "" {
		_, err := mainAgent.Run(ctx, oneShot)
		return err
	}
	return repl(ctx, mainAgent, bufio.NewReader(os.Stdin))
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
