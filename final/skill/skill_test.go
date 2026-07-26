package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	skills, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
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

	skills, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
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

	skills, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	body := skills[0].Body
	for _, want := range []string{"前半", "---", "後半"} {
		if !strings.Contains(body, want) {
			t.Errorf("本文から %q が失われた: %q", want, body)
		}
	}
}

func TestLoadWithoutSkillsDir(t *testing.T) {
	skills, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("スキルディレクトリがなくてもエラーにしない: %v", err)
	}
	if len(skills) != 0 {
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

	skills, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("有効なスキルは2件のはず: %d件 %+v", len(skills), skills)
	}
	if skills[0].Name != "alpha" || skills[1].Name != "zebra" {
		t.Errorf("名前順に並ぶべき: %+v", skills)
	}
}

// スキルの仕組みの核心: 本文は skill ツールを呼んだときだけ手に入る。
func TestToolReturnsBodyOnDemand(t *testing.T) {
	skills := []Skill{
		{Name: "commit", Description: "コミットの書き方", Body: "1行目に要約を書く"},
		{Name: "review", Description: "レビューの観点", Body: "命名とエラー処理を見る"},
	}
	tool := NewTool(skills)
	if tool == nil {
		t.Fatal("スキルがあるのに nil が返った")
	}

	got, err := tool.Run(context.Background(), json.RawMessage(`{"name":"commit"}`))
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
	_, err := tool.Run(context.Background(), json.RawMessage(`{"name":"nosuch"}`))
	if err == nil {
		t.Fatal("存在しないスキルはエラーになるべき")
	}
	// エラーに候補を含める = LLMが自力で立て直せる。
	if !strings.Contains(err.Error(), "commit") {
		t.Errorf("エラーに利用可能なスキルが載っていない: %v", err)
	}
}

func TestNewToolWithoutSkillsReturnsNil(t *testing.T) {
	// スキルが1つもなければツール自体をLLMに見せない。
	if tool := NewTool(nil); tool != nil {
		t.Error("スキル0件では nil を返すべき")
	}
}
