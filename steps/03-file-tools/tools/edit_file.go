package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EditFile はファイルを編集するツール。これが入ると
// 「読むだけ」のエージェントが「コードを書ける」エージェントに変わる。
//
// 編集方式は「old_str を new_str に置き換える」方式を採用している。
// 行番号指定ではなく文字列一致にするのは、LLMが行番号の計算を
// 間違えやすい一方、「いま見た内容をそのまま引用する」のは得意だからだ。
// old_str がファイル内で一意でなければエラーにする(どこを置き換えるか
// 曖昧なまま書き換えるより、LLMに引用を長くしてもらう方が安全)。
type EditFile struct {
	workDir string
}

// NewEditFile は workDir を基準にパスを解決する edit_file を作る。
func NewEditFile(workDir string) *EditFile { return &EditFile{workDir: workDir} }

func (*EditFile) Name() string { return "edit_file" }

func (*EditFile) Description() string {
	return "ファイルを編集します。old_str をファイル内で検索し、new_str に置き換えます。" +
		"old_str はファイル内でちょうど1回だけ出現する必要があります(0回・複数回はエラー)。" +
		"新規ファイルを作るときは old_str を空文字にして、new_str に全内容を渡してください。"
}

func (*EditFile) InputSchema() Schema {
	return Schema{
		Properties: map[string]any{
			"path":    StringProperty("編集するファイルの相対パス"),
			"old_str": StringProperty("置き換え対象の文字列。ファイル内で一意になるよう前後の文脈を含めること。新規作成時は空文字。"),
			"new_str": StringProperty("置き換え後の文字列"),
		},
		Required: []string{"path", "old_str", "new_str"},
	}
}

func (e *EditFile) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path   string `json:"path"`
		OldStr string `json:"old_str"`
		NewStr string `json:"new_str"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("入力のパースに失敗: %w", err)
	}
	if in.Path == "" {
		return "", errors.New("path を指定してください")
	}
	if in.OldStr == in.NewStr {
		return "", errors.New("old_str と new_str が同じです")
	}
	path := filepath.Join(e.workDir, in.Path)

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist) && in.OldStr == "":
		// 新規作成: old_str が空 かつ ファイルが存在しない場合のみ。
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(in.NewStr), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s を新規作成しました", in.Path), nil
	case err != nil:
		return "", err
	}

	content := string(data)
	switch n := strings.Count(content, in.OldStr); {
	case in.OldStr == "":
		return "", fmt.Errorf("%s は既に存在します。編集するには old_str を指定してください", in.Path)
	case n == 0:
		return "", errors.New("old_str がファイル内に見つかりません。read_file で最新の内容を確認してください")
	case n > 1:
		return "", fmt.Errorf("old_str がファイル内に %d 回出現します。前後の文脈を含めて一意にしてください", n)
	}

	updated := strings.Replace(content, in.OldStr, in.NewStr, 1)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s を編集しました", in.Path), nil
}
