package skill

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/toshi0607/go-coding-agent-handson/steps/10-streaming-hooks/tools"
)

// writeSkill はテスト用にスキルを1つ配置する。
func writeSkill(t *testing.T, workDir, name, content string) {
	t.Helper()
	dir := filepath.Join(workDir, SkillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, EntryFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// loadFrom は dir を作業ディレクトリとしてスキルを読み込む。
func loadFrom(t *testing.T, dir string) []Skill {
	t.Helper()
	skills, warnings := loadWithWarnings(t, dir)
	if warnings != "" {
		t.Logf("警告: %s", warnings)
	}
	return skills
}

func loadWithWarnings(t *testing.T, dir string) ([]Skill, string) {
	t.Helper()
	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	var warn strings.Builder
	skills, err := Load(ws, &warn)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return skills, warn.String()
}

func TestLoadParsesFrontmatterAndBody(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "commit", `---
name: commit-message
description: コミットメッセージの書き方
---

## 手順

1. 変更内容を確認する
2. 1行目に要約を書く
`)

	skills := loadFrom(t, dir)
	if len(skills) != 1 {
		t.Fatalf("スキルは1件のはず: %d件", len(skills))
	}
	s := skills[0]
	// フロントマターの name がディレクトリ名より優先される。
	if s.Name != "commit-message" {
		t.Errorf("Name: got %q", s.Name)
	}
	if s.Description != "コミットメッセージの書き方" {
		t.Errorf("Description: got %q", s.Description)
	}
	if !strings.HasPrefix(s.Body, "## 手順") {
		t.Errorf("Body がフロントマター直後から始まっていない: %q", s.Body)
	}
	if strings.Contains(s.Body, "description:") {
		t.Errorf("Body にフロントマターが混入している: %q", s.Body)
	}
}

func TestLoadFallsBackToDirectoryName(t *testing.T) {
	dir := t.TempDir()
	// フロントマターなし。
	writeSkill(t, dir, "review", "コードレビューの観点を列挙する。\n")

	skills := loadFrom(t, dir)
	if len(skills) != 1 || skills[0].Name != "review" {
		t.Fatalf("ディレクトリ名がスキル名になるべき: %+v", skills)
	}
	if !strings.Contains(skills[0].Body, "コードレビュー") {
		t.Errorf("フロントマターなしでも本文が読めるべき: %q", skills[0].Body)
	}
}

// 本文中の水平線をフロントマターと誤認しないこと。
func TestLoadDoesNotTreatBodyHorizontalRuleAsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "notes", "前半\n\n---\n\n後半\n")

	skills := loadFrom(t, dir)
	body := skills[0].Body
	for _, want := range []string{"前半", "---", "後半"} {
		if !strings.Contains(body, want) {
			t.Errorf("本文から %q が失われた: %q", want, body)
		}
	}
}

func TestLoadWithoutSkillsDir(t *testing.T) {
	if skills := loadFrom(t, t.TempDir()); len(skills) != 0 {
		t.Errorf("スキルは0件のはず: %+v", skills)
	}
}

func TestLoadSortsAndSkipsInvalidEntries(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "zebra", "z")
	writeSkill(t, dir, "alpha", "a")
	// SKILL.md がないディレクトリは無視される。
	if err := os.MkdirAll(filepath.Join(dir, SkillsDir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	// ディレクトリでないファイルも無視される。
	if err := os.WriteFile(filepath.Join(dir, SkillsDir, "stray.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := loadFrom(t, dir)
	if len(skills) != 2 {
		t.Fatalf("有効なスキルは2件のはず: %d件 %+v", len(skills), skills)
	}
	if skills[0].Name != "alpha" || skills[1].Name != "zebra" {
		t.Errorf("名前順に並ぶべき: %+v", skills)
	}
}

// スキルは「リポジトリのファイルを読んでプロンプトに載せる」機能なので、
// 封じ込めを通さないと外部ファイルの中身がLLMに漏れる。
// しかもスキルは無条件許可なので承認プロンプトすら出ない。
func TestLoadIsContained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windowsではシンボリックリンクの作成に特権が必要")
	}

	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "credentials")
	if err := os.WriteFile(secret, []byte("aws_secret_access_key = TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("SKILL.mdが外部へのシンボリックリンク", func(t *testing.T) {
		dir := t.TempDir()
		skillDir := filepath.Join(dir, SkillsDir, "helper")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, filepath.Join(skillDir, EntryFile)); err != nil {
			t.Fatal(err)
		}

		skills, warnings := loadWithWarnings(t, dir)
		for _, s := range skills {
			if strings.Contains(s.Body, "TOPSECRET") || strings.Contains(s.Description, "TOPSECRET") {
				t.Fatalf("外部ファイルの内容が漏れた: %+v", s)
			}
		}
		if warnings == "" {
			t.Error("拒否したことを警告で知らせるべき")
		}
	})

	t.Run("スキルディレクトリ全体が外部へのシンボリックリンク", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".agent"), 0o755); err != nil {
			t.Fatal(err)
		}
		// .agent/skills 自体を外部ディレクトリへのリンクにする。
		outsideSkills := t.TempDir()
		evil := filepath.Join(outsideSkills, "evil")
		if err := os.MkdirAll(evil, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evil, EntryFile),
			[]byte("---\nname: evil\ndescription: 外部の説明\n---\nBODY-FROM-OUTSIDE"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideSkills, filepath.Join(dir, SkillsDir)); err != nil {
			t.Fatal(err)
		}

		ws, err := tools.NewWorkspace(dir)
		if err != nil {
			t.Fatal(err)
		}
		skills, err := Load(ws, io.Discard)
		// 拒否はエラーでも空でもよいが、内容が返ってはいけない。
		if err == nil {
			for _, s := range skills {
				if strings.Contains(s.Body, "BODY-FROM-OUTSIDE") || strings.Contains(s.Description, "外部の説明") {
					t.Fatalf("外部ディレクトリのスキルが読み込まれた: %+v", s)
				}
			}
		}
	})
}

// SKILL.mdの書き間違いを黙って通すと、LLMに空文字や
// フロントマター混じりの本文が渡る。エラーで気づかせる。
func TestLoadSkipsMalformedSkills(t *testing.T) {
	cases := map[string]string{
		"フロントマター閉じ忘れ": "---\nname: broken\ndescription: 閉じてない\n本文",
		"本文が空":        "---\nname: empty\ndescription: 本文なし\n---\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeSkill(t, dir, "target", content)

			skills, warnings := loadWithWarnings(t, dir)
			if len(skills) != 0 {
				t.Errorf("不正なスキルは読み込まないべき: %+v", skills)
			}
			if warnings == "" {
				t.Error("読み込めなかったことを警告すべき")
			}
		})
	}
}

// BOM付きで保存すると1行目が "---" と一致せず、
// フロントマターが本文に丸ごと漏れる。Windowsのエディタで踏みやすい。
func TestLoadHandlesBOM(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "bomdir", "\ufeff---\nname: bom\ndescription: BOM付き\n---\n本文です")

	skills := loadFrom(t, dir)
	if len(skills) != 1 {
		t.Fatalf("スキルは1件のはず: %+v", skills)
	}
	s := skills[0]
	if s.Name != "bom" || s.Description != "BOM付き" {
		t.Errorf("BOM付きでフロントマターが解釈されていない: %+v", s)
	}
	if strings.Contains(s.Body, "description:") {
		t.Errorf("本文にフロントマターが漏れた: %q", s.Body)
	}
}

func TestLoadHandlesCRLF(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "crlf", "---\r\nname: crlf\r\ndescription: CRLF改行\r\n---\r\n本文\r\n")

	skills := loadFrom(t, dir)
	if len(skills) != 1 || skills[0].Description != "CRLF改行" {
		t.Fatalf("CRLFが処理されていない: %+v", skills)
	}
	if strings.Contains(skills[0].Body, "\r") {
		t.Errorf("本文にCRが残っている: %q", skills[0].Body)
	}
}

// 巨大な1行があっても本文が黙って切り詰められないこと。
// bufio.Scanner を使っていると scanner.Err() の見落としでここが壊れる
// (1行が長すぎるとスキャンが止まり、以降の本文が無言で消える)。
func TestLoadHandlesVeryLongLine(t *testing.T) {
	dir := t.TempDir()
	// 読み込み上限には収まるが、bufio.Scanner のデフォルト上限
	// (64KB)は大きく超える長さ。
	long := strings.Repeat("a", 200*1024)
	writeSkill(t, dir, "long", "先頭マーカー\n"+long+"\n末尾マーカー")

	skills := loadFrom(t, dir)
	if len(skills) != 1 {
		t.Fatalf("スキルは1件のはず: %+v", skills)
	}
	// 末尾まで読めていること(途中で止まっていない)。
	if !strings.Contains(skills[0].Body, "末尾マーカー") {
		t.Error("長い行のあとの本文が失われた")
	}
	if len(skills[0].Body) < 200*1024 {
		t.Errorf("本文が想定より短い(黙って切り詰められた可能性): %dバイト", len(skills[0].Body))
	}
}

// 読み込み上限を超えたら、切り詰めではなくエラーにすること。
// 途中で切るとフロントマターの閉じを読み落とし、無関係なエラーになる。
func TestLoadRejectsOversizedSkill(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "huge", "---\nname: huge\ndescription: 巨大\n---\n"+strings.Repeat("a", maxBodyBytes+1))

	skills, warnings := loadWithWarnings(t, dir)
	if len(skills) != 0 {
		t.Errorf("上限超過のスキルは読み込まないべき: %+v", skills)
	}
	if !strings.Contains(warnings, "大きすぎます") {
		t.Errorf("上限超過であることが分かる警告を出すべき: %q", warnings)
	}
}

// 説明が無制限だと「目次だけ載せるから軽い」という前提が崩れる。
func TestLoadTruncatesLongDescription(t *testing.T) {
	dir := t.TempDir()
	// 読み込み上限には収まるが、目次1行としては非常識に長い説明。
	huge := strings.Repeat("説", 5_000)
	writeSkill(t, dir, "verbose", "---\nname: verbose\ndescription: "+huge+"\n---\n本文")

	skills := loadFrom(t, dir)
	if len(skills) != 1 {
		t.Fatalf("スキルは1件のはず: %+v", skills)
	}
	if n := len([]rune(skills[0].Description)); n > maxDescriptionChars+1 {
		t.Errorf("説明が切り詰められていない: %d文字", n)
	}
}

// 名前が衝突すると片方が到達不能になる。静かに壊れないこと。
func TestLoadWarnsOnDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "one", "---\nname: same\ndescription: 1つ目\n---\nBODY-one")
	writeSkill(t, dir, "two", "---\nname: same\ndescription: 2つ目\n---\nBODY-two")

	skills, warnings := loadWithWarnings(t, dir)
	if len(skills) != 1 {
		t.Errorf("重複した名前は1件だけ採用されるべき: %+v", skills)
	}
	if warnings == "" {
		t.Error("名前の重複を警告すべき")
	}
}

// 読めないSKILL.mdが1つあってもエージェントは起動できること。
// スキルは任意機能なので、壊れた1件で全体を止めない。
func TestLoadContinuesOnUnreadableSkill(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("rootではパーミッションによる読み取り拒否が起きない")
	}
	dir := t.TempDir()
	writeSkill(t, dir, "good", "---\nname: good\ndescription: 正常\n---\n本文")

	// 読めないSKILL.mdを置く。
	badDir := filepath.Join(dir, SkillsDir, "bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	badFile := filepath.Join(badDir, EntryFile)
	if err := os.WriteFile(badFile, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}

	skills, warnings := loadWithWarnings(t, dir)
	if len(skills) != 1 || skills[0].Name != "good" {
		t.Errorf("正常なスキルは読み込まれるべき: %+v", skills)
	}
	if warnings == "" {
		t.Error("読めなかったスキルを警告すべき")
	}
}

// スキルの仕組みの核心: 本文は skill ツールを呼んだときだけ手に入る。
func TestToolReturnsBodyOnDemand(t *testing.T) {
	skills := []Skill{
		{Name: "commit", Description: "コミットの書き方", Body: "1行目に要約を書く"},
		{Name: "review", Description: "レビューの観点", Body: "命名とエラー処理を見る"},
	}
	tool := NewTool(skills)

	got, err := tool.Run(t.Context(), json.RawMessage(`{"name":"commit"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "1行目に要約を書く" {
		t.Errorf("本文が返らない: %q", got)
	}

	// ツールの説明にはスキル名の一覧が載る(LLMが名前を知るため)。
	desc := tool.Description()
	for _, name := range []string{"commit", "review"} {
		if !strings.Contains(desc, name) {
			t.Errorf("ツール説明に %q がない: %q", name, desc)
		}
	}
	// 説明に本文は載らない(載せたら段階的開示の意味がない)。
	if strings.Contains(desc, "1行目に要約を書く") {
		t.Errorf("ツール説明に本文が混入している: %q", desc)
	}
}

func TestToolUnknownSkill(t *testing.T) {
	tool := NewTool([]Skill{{Name: "commit", Body: "x"}})
	_, err := tool.Run(t.Context(), json.RawMessage(`{"name":"nosuch"}`))
	if err == nil {
		t.Fatal("存在しないスキルはエラーになるべき")
	}
	// エラーに候補を含める = LLMが自力で立て直せる。
	if !strings.Contains(err.Error(), "commit") {
		t.Errorf("エラーに利用可能なスキルが載っていない: %v", err)
	}
}
