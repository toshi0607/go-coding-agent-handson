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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// Load は作業ディレクトリ配下のスキルをすべて読み込む。
// ディレクトリがなければ空を返す(スキルは任意)。
//
// 起動時に本文まで読んでしまうが、これはメモリ上に置くだけで、
// LLMに送るのは Description だけである。「読み込むタイミング」と
// 「コンテキストに載せるタイミング」を分けているのが要点。
func Load(workDir string) ([]Skill, error) {
	dir := filepath.Join(workDir, SkillsDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), EntryFile))
		if os.IsNotExist(err) {
			continue // SKILL.md がないディレクトリは無視
		}
		if err != nil {
			return nil, err
		}
		s := parse(string(data))
		if s.Name == "" {
			s.Name = e.Name() // フロントマターに name がなければディレクトリ名
		}
		if s.Description == "" {
			s.Description = "(説明なし)"
		}
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
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
func parse(content string) Skill {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var s Skill
	var body strings.Builder
	inFrontmatter := false
	frontmatterDone := false

	for lineNo := 0; scanner.Scan(); lineNo++ {
		line := scanner.Text()

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

	s.Body = strings.TrimRight(body.String(), "\n")
	return s
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
// スキルが1つもなければ nil を返す(空のツールをLLMに見せない)。
func NewTool(skills []Skill) *Tool {
	if len(skills) == 0 {
		return nil
	}
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
