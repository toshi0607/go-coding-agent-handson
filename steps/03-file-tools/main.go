// go-coding-agent-handson のミニコーディングエージェント(step 03 時点)。
//
// ここ(main)の仕事は「組み立て」である。各パッケージが提供するパーツ
// (LLMクライアント、ツール)をつなぎ合わせてエージェントを作り、REPLに渡す。
// どのパーツに何を渡しているかを追うと、エージェント全体の構造が見える。
//
// 使い方:
//
//	export ANTHROPIC_API_KEY=...
//	go run ./steps/03-file-tools   # 対話モード(REPL)
//	go run ./steps/03-file-tools -p "質問"   # ワンショットモード
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/toshi0607/go-coding-agent-handson/steps/03-file-tools/agent"
	"github.com/toshi0607/go-coding-agent-handson/steps/03-file-tools/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/03-file-tools/tools"
)

// defaultModel は使用するモデル。環境変数 ANTHROPIC_MODEL で上書きできる。
const defaultModel = anthropic.Model("claude-opus-5-5")

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
		tools.NewListFiles(workDir),
		tools.NewEditFile(workDir),
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
	return repl(ctx, mainAgent, newLineReader(os.Stdin))
}

// repl は対話ループ。入力を読み、エージェントに渡す。
func repl(ctx context.Context, a *agent.Agent, stdin *lineReader) error {
	fmt.Println("ミニコーディングエージェント(Ctrl-D で終了)")
	for {
		fmt.Print("\n> ")
		line, err := stdin.ReadLine(ctx)
		// 最後の行に改行がないと、ReadString はその行と io.EOF を同時に返す。
		// 行が空のときだけ終了し、空でなければその行を処理してから次で終わる。
		if errors.Is(err, io.EOF) && line == "" {
			fmt.Println()
			return nil
		}
		if ctx.Err() != nil {
			fmt.Println()
			return nil // 入力待ちの間の Ctrl-C
		}
		if err != nil && !errors.Is(err, io.EOF) {
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

// lineReader は標準入力を別のgoroutineで1行ずつ読む。
//
// bufio.Reader の ReadString は、Ctrl-C で ctx がキャンセルされても
// 戻らない。入力待ちの間に Ctrl-C を押しても終了しないのはそのため。
// 読み取りを別のgoroutineに任せ、ReadLine は「行が届く」と
// 「ctx がキャンセルされる」の早い方を待つ。
type lineReader struct {
	lines chan lineResult
}

type lineResult struct {
	line string
	err  error
}

func newLineReader(r io.Reader) *lineReader {
	lr := &lineReader{lines: make(chan lineResult)}
	go func() {
		br := bufio.NewReader(r)
		for {
			line, err := br.ReadString('\n')
			lr.lines <- lineResult{line, err}
			if err != nil {
				close(lr.lines)
				return
			}
		}
	}()
	return lr
}

// ReadLine は1行を返す。ctx がキャンセルされたら ctx.Err() を返す。
// bufio.Reader の ReadString と同じく、最後の行に改行がなければ
// その行と io.EOF を同時に返す。
func (lr *lineReader) ReadLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r, ok := <-lr.lines:
		if !ok {
			return "", io.EOF
		}
		return r.line, r.err
	}
}
