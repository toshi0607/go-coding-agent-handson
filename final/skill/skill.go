// Package skill はスキル(専門知識のパッケージ化)を実装する。
//
// スキルとは「特定の作業のやり方を書いたMarkdownファイル」である。
// スラッシュコマンドと似ているが、起動する主体が違う:
//
//	スラッシュコマンド … ユーザーが明示的に呼ぶ(/explain main.go)
//	スキル       … LLMが「いま必要だ」と判断して自分で読む
//
// なぜファイルに切り出すのか。専門知識をすべてシステムプロンプトに
// 書き込めば確実にLLMに届くが、使わない知識も毎ターン送られるため、
// コンテキストとコストを食い続ける。逆に必要なときだけ読ませたいが、
// LLMは存在を知らないものを読もうとはしない。
//
// この矛盾を解くのが「段階的開示(progressive disclosure)」である:
//
//  1. システムプロンプトには名前と1行説明だけを載せる(数十トークン)
//  2. LLMが関連すると判断したら skill ツールで本文を読む(数千トークン)
//
// 目次だけ常に見せて、中身は必要になってから開く。人間が分厚い
// マニュアルを扱うときと同じやり方である。
package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/toshi0607/go-coding-agent-handson/final/tools"
)

// SkillsDir はスキルの置き場所(作業ディレクトリ基準)。
// 1スキル = 1ディレクトリ = <名前>/SKILL.md とする。
// ディレクトリにしておくと、スキルが参照する補助ファイル
// (スクリプトやテンプレート)を同じ場所に置ける。
const SkillsDir = ".agent/skills"

// EntryFile は各スキルディレクトリ内の本体ファイル名。
const EntryFile = "SKILL.md"

// Skill は1つのスキル。
type Skill struct {
	// Name はスキル名(ディレクトリ名、またはフロントマターの name)。
	Name string
	// Description は1行説明。これだけがシステムプロンプトに載る。
	Description string
	// Body は本文。LLMが skill ツールを呼んだときに初めて渡される。
	Body string
}

const (
	// maxDescriptionChars は説明の長さ上限。
	// 説明はシステムプロンプトに毎ターン載るので、ここが無制限だと
	// 「目次だけ載せるから軽い」という前提が崩れる。
	maxDescriptionChars = 200

	// maxBodyBytes は本文の読み込み上限。
	maxBodyBytes = 256 * 1024
)

// Load は作業ディレクトリ配下のスキルをすべて読み込む。
// ディレクトリがなければ空を返す(スキルは任意)。
//
// パスは必ず Workspace 経由で解決する。スキルは
// 「リポジトリに置かれたファイルを読んでプロンプトに載せる」機能で、
// SKILL.md が作業ディレクトリの外を指すシンボリックリンクだと、
// 外部ファイルの中身がそのままLLMに渡ってしまう。しかもスキルの
// 読み取りは承認フローを通していない(無条件許可)ため、
// 封じ込めを外すと確認プロンプトすら出ない。
//
// 「検証を各ツールに任せると必ずどれかで忘れる」という Workspace の
// 設計意図はこのパッケージにも当てはまる。ファイルを読む機能を
// 追加するたびに、封じ込めを通したかを確認すること。
//
// 起動時に本文まで読んでしまうが、これはメモリ上に置くだけで、
// LLMに送るのは Description だけである。「読み込むタイミング」と
// 「コンテキストに載せるタイミング」を分けているのが要点。
//
// 個別スキルの読み込み失敗はエラーにせず、警告を warn に書いて
// そのスキルだけ飛ばす。スキルは任意機能なので、1つの壊れた
// SKILL.md でエージェント全体が起動しなくなる方が困る。
func Load(ws *tools.Workspace, warn io.Writer) ([]Skill, error) {
	dir, err := ws.Resolve(SkillsDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		fmt.Fprintf(warn, "警告: スキルディレクトリを読めません: %v\n", err)
		return nil, nil
	}

	var skills []Skill
	seen := map[string]string{} // スキル名 → 由来のディレクトリ名
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := loadOne(ws, e.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue // SKILL.md がないディレクトリは無視
		}
		if err != nil {
			fmt.Fprintf(warn, "警告: スキル %s を読み込めません: %v\n", e.Name(), err)
			continue
		}
		// 名前が衝突すると、片方が黙って到達不能になる。
		// 静かに壊れるより警告して飛ばす。
		if from, dup := seen[s.Name]; dup {
			fmt.Fprintf(warn, "警告: スキル名 %q が %s と %s で重複しています。%s を無視します\n",
				s.Name, from, e.Name(), e.Name())
			continue
		}
		seen[s.Name] = e.Name()
		skills = append(skills, s)
	}
	slices.SortFunc(skills, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return skills, nil
}

// loadOne は1つのスキルディレクトリを読み込む。
func loadOne(ws *tools.Workspace, dirName string) (Skill, error) {
	path, err := ws.Resolve(filepath.Join(SkillsDir, dirName, EntryFile))
	if err != nil {
		return Skill{}, err
	}
	data, err := readLimited(path, maxBodyBytes)
	if err != nil {
		return Skill{}, err
	}
	s, err := parse(string(data))
	if err != nil {
		return Skill{}, err
	}
	if s.Name == "" {
		s.Name = dirName // フロントマターに name がなければディレクトリ名
	}
	if s.Description == "" {
		s.Description = "(説明なし)"
	}
	s.Description = truncate(s.Description, maxDescriptionChars)
	return s, nil
}

// readLimited はファイルを読む。上限を超えていればエラーにする。
//
// 上限で黙って切るのではなくエラーにするのが要点。途中で切ると
// フロントマターの閉じ "---" を読み落として「フロントマターが
// 閉じられていません」という無関係なエラーになり、原因が分からなくなる。
// 制限に当たったことは制限に当たったと伝えるべきである。
func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 上限+1バイト読んで、超過を検出する。
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s が大きすぎます(上限 %dバイト)", EntryFile, limit)
	}
	return data, nil
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// parse はSKILL.mdをフロントマターと本文に分ける。
//
// 形式:
//
//	---
//	name: commit-message
//	description: コミットメッセージの書き方
//	---
//	(ここから本文)
//
// YAMLライブラリを使わず自前で数行のパースにしているのは、
// 教材として依存を増やしたくないから。実務ならYAMLパーサでよい。
//
// bufio.Scanner を使わず strings.Split で行分割しているのは、
// Scanner には「1行が長すぎると途中で止まる」制限があり、
// scanner.Err() を見忘れると本文が黙って切り詰められるため。
// 「エラーを見落としたときに静かに壊れる」APIは避けるのが安全。
func parse(content string) (Skill, error) {
	// UTF-8のBOM(U+FEFF)を除去する。これがあると1行目が "---" と
	// 一致せず、フロントマターが本文に丸ごと漏れる。
	// Windowsのエディタで保存すると付くことがある。
	const bom = "\ufeff"
	content = strings.TrimPrefix(content, bom)

	var s Skill
	var body strings.Builder
	inFrontmatter := false
	frontmatterDone := false

	lines := strings.Split(content, "\n")
	for lineNo, line := range lines {
		line = strings.TrimSuffix(line, "\r") // CRLF対応

		// 1行目の "---" だけをフロントマターの開始と認める。
		// 本文中の水平線("---")を誤ってフロントマターと解釈しないため。
		if lineNo == 0 && strings.TrimSpace(line) == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				frontmatterDone = true
				continue
			}
			key, value, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			switch strings.TrimSpace(key) {
			case "name":
				s.Name = strings.TrimSpace(value)
			case "description":
				s.Description = strings.TrimSpace(value)
			}
			continue
		}
		// フロントマター直後の空行1つは本文に含めない(見た目の調整)。
		if frontmatterDone && strings.TrimSpace(line) == "" && body.Len() == 0 {
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}

	// フロントマターが閉じられていないと本文が空になり、LLMに空文字を
	// 返すことになる。書き間違いを黙って通さず、エラーで気づかせる。
	if inFrontmatter {
		return Skill{}, errors.New("フロントマターが --- で閉じられていません")
	}

	s.Body = strings.TrimRight(body.String(), "\n")
	if s.Body == "" {
		return Skill{}, errors.New("本文が空です")
	}
	return s, nil
}

// Tool は skill ツール。LLMがスキルの本文を読むために使う。
//
// これがスキルの仕組みの中核である。エージェント側から見れば
// 「名前を受け取って本文を返すだけ」のごく単純なツールだが、
// このツールがあることで初めて「必要なときだけ読む」が成立する。
type Tool struct {
	byName map[string]Skill
	names  []string
}

// NewTool は読み込んだスキルから skill ツールを作る。
//
// スキルが0件のときに nil を返す設計にはしていない。
// Goでは具体型のnilポインタをインターフェースに入れると
// 「非nilのインターフェース値」になり、`tool != nil` の判定を
// すり抜けてメソッド呼び出しでpanicする。よく踏まれる罠なので、
// 「0件かどうか」は呼び出し側が len(skills) で判定する。
func NewTool(skills []Skill) *Tool {
	t := &Tool{byName: make(map[string]Skill, len(skills))}
	for _, s := range skills {
		t.byName[s.Name] = s
		t.names = append(t.names, s.Name)
	}
	return t
}

func (*Tool) Name() string { return "skill" }

func (t *Tool) Description() string {
	return "スキル(特定の作業のやり方をまとめた手順書)の本文を読み込みます。" +
		"システムプロンプトに列挙されたスキルのうち、いま取り組んでいる作業に" +
		"関連するものがあれば、作業を始める前にこのツールで本文を読んでください。" +
		"利用可能なスキル: " + strings.Join(t.names, ", ")
}

func (*Tool) InputSchema() tools.Schema {
	return tools.Schema{
		Properties: map[string]any{
			"name": tools.StringProperty("読み込むスキルの名前"),
		},
		Required: []string{"name"},
	}
}

func (t *Tool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("入力のパースに失敗: %w", err)
	}
	s, ok := t.byName[in.Name]
	if !ok {
		return "", fmt.Errorf("スキル %q は存在しません。利用可能なスキル: %s", in.Name, strings.Join(t.names, ", "))
	}
	return s.Body, nil
}
