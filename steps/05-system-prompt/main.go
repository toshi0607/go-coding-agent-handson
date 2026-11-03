// go-coding-agent-handson のミニコーディングエージェント(step 05 時点)。
//
// ここ(main)の仕事は「組み立て」である。各パッケージが提供するパーツ
// (LLMクライアント、ツール、権限、コンテキスト管理)をつなぎ合わせて
// エージェントを作り、REPLに渡す。どのパーツに何を渡しているかを追うと、
// エージェント全体の構造が見える。
//
// 使い方:
//
//	export ANTHROPIC_API_KEY=...
//	go run ./steps/05-system-prompt   # 対話モード(REPL)
//	go run ./steps/05-system-prompt -p "質問"   # ワンショットモード
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
	"github.com/toshi0607/go-coding-agent-handson/steps/05-system-prompt/agent"
	"github.com/toshi0607/go-coding-agent-handson/steps/05-system-prompt/config"
	"github.com/toshi0607/go-coding-agent-handson/steps/05-system-prompt/llm"
	"github.com/toshi0607/go-coding-agent-handson/steps/05-system-prompt/permission"
	"github.com/toshi0607/go-coding-agent-handson/steps/05-system-prompt/prompt"
	"github.com/toshi0607/go-coding-agent-handson/steps/05-system-prompt/tools"
)

// defaultModel は使用するモデル。環境変数 ANTHROPIC_MODEL で上書きできる。
const defaultModel = anthropic.Model("claude-opus-5-5")

func main() {
	oneShot := flag.String("p", "", "対話モードの代わりに、この1つの依頼を実行して終了する")
	flag.Parse()

	// 入力待ちの間の Ctrl-C は、エラーではなく通常の終了として扱う。
	if err := run(*oneShot); err != nil && !errors.Is(err, context.Canceled) {
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

	stdin := newLineReader(os.Stdin)
	asker := makeAsker(ctx, stdin, "[y]es / [n]o / [a]lways")

	// --- ファイル操作の範囲を作業ディレクトリに限定する ---
	// ファイル系ツールはこの Workspace 経由でしかパスを解決できない。
	// LLMが渡してきたパスを直接OSに渡さないためのしくみである。
	ws, err := tools.NewWorkspace(workDir)
	if err != nil {
		return err
	}

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
			// Workspace で作業ディレクトリの中に限定してあるからである。
			// 作業ディレクトリへの限定がなければ「読み取りだから安全」は成り立たない
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
	systemPrompt := prompt.Build(prompt.Options{Workspace: ws})

	mainAgent := agent.New(agent.Config{
		Client:       client,
		Model:        model,
		SystemPrompt: systemPrompt,
		Tools:        baseTools,
		Permission:   checker,
		Out:          os.Stdout,
	})

	if oneShot != "" {
		_, err := mainAgent.Run(ctx, oneShot)
		return err
	}
	return repl(ctx, mainAgent, stdin)
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

// makeAsker は標準入力から承認を得る Asker を作る。
// choices は選択肢の表示("[y]es / [n]o" など)。
func makeAsker(ctx context.Context, stdin *lineReader, choices string) permission.Asker {
	return func(question string) (string, error) {
		fmt.Printf("\n%s\n  %s > ", question, choices)
		answer, err := stdin.ReadLine(ctx)
		// 最後の行に改行がないと、その行と io.EOF が同時に返る。
		// 行があれば、それを答えとして使う。
		if errors.Is(err, io.EOF) && answer != "" {
			err = nil
		}
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(answer), nil
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
// キャンセルの後も、読み取り用のgoroutineは次の行を待ったまま残る。
// プロセスはそのまま終了するので、実害はない。
// bufio.Reader の ReadString と同じく、最後の行に改行がなければ
// その行と io.EOF を同時に返す。
func (lr *lineReader) ReadLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r, ok := <-lr.lines:
		// 行の受信とキャンセルが同時に起きると、select はどちらを選ぶか
		// 決まっていない。承認の入力をキャンセルの後に受け取らないよう、
		// 受信した後にもキャンセルを確かめる。
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !ok {
			return "", io.EOF
		}
		return r.line, r.err
	}
}
