//go:build ignore

// sync_check は「step 10 の解答は final そのもの」という不変条件を検査する。
//
//	go run scripts/sync_check.go <finalディレクトリ> <stepディレクトリ> <穴のマーカー>
//
// 素朴に「穴のあるファイルは丸ごと比較対象外」とすると、穴と同じファイルに
// ある他の関数(たとえば hooks.go の RunPost や run)の食い違いを見逃す。
// finalにバグ修正を入れてstepsへの反映を忘れる、という今回防ぎたい事故は
// まさにその形で起こりうる。
//
// そこでファイル単位ではなく**トップレベル宣言の単位**で比較する:
//
//   - 穴のある宣言(マーカーを含む関数など)だけを両側から取り除く
//   - import宣言も取り除く。穴を開けると使わなくなるimportがあり、
//     そこは食い違って当然である(コンパイルが保証してくれる領域でもある)
//   - 残り全部を比較する。宣言の追加・削除も検出する
//
// 比較の前に、step側のパス(importパスとコメント中の ./steps/...)を
// final側の表記に揃える。
package main

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const modulePath = "github.com/toshi0607/go-coding-agent-handson"

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/sync_check.go <finalDir> <stepDir> <marker>")
		os.Exit(2)
	}
	finalDir, stepDir, marker := os.Args[1], os.Args[2], os.Args[3]

	problems, err := compareTrees(finalDir, stepDir, marker)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ 検査を実行できませんでした: %v\n", err)
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Printf("  ✗ %s\n", p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
	fmt.Printf("  ✓ %s は %s と一致しています(穴のある宣言を除く)\n", stepDir, finalDir)
}

func compareTrees(finalDir, stepDir, marker string) ([]string, error) {
	var problems []string

	finalFiles, err := goFiles(finalDir)
	if err != nil {
		return nil, err
	}
	stepFiles, err := goFiles(stepDir)
	if err != nil {
		return nil, err
	}

	// 双方向の存在確認。step側のテストゲートだけは step にしか無い。
	for rel := range finalFiles {
		if !stepFiles[rel] {
			problems = append(problems, fmt.Sprintf("%s が %s にありません(%s にはあります)", rel, stepDir, finalDir))
		}
	}
	for rel := range stepFiles {
		if filepath.Base(rel) == "step_gate_test.go" {
			continue
		}
		if !finalFiles[rel] {
			problems = append(problems, fmt.Sprintf("%s が %s にありません(%s にはあります)", rel, finalDir, stepDir))
		}
	}

	var rels []string
	for rel := range finalFiles {
		if stepFiles[rel] {
			rels = append(rels, rel)
		}
	}
	slices.Sort(rels)

	for _, rel := range rels {
		diff, err := compareFile(filepath.Join(finalDir, rel), filepath.Join(stepDir, rel), finalDir, stepDir, marker)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		if diff != "" {
			problems = append(problems, fmt.Sprintf("%s が %s と食い違っています(%s の修正を反映し忘れていませんか)\n%s",
				rel, finalDir, finalDir, diff))
		}
	}
	return problems, nil
}

func goFiles(dir string) (map[string]bool, error) {
	files := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[rel] = true
		return nil
	})
	return files, err
}

// compareFile は穴のある宣言とimportを伏せたうえで2ファイルを比較する。
// 一致すれば空文字、違えば差分の抜粋を返す。
func compareFile(finalPath, stepPath, finalDir, stepDir, marker string) (string, error) {
	finalSrc, err := os.ReadFile(finalPath)
	if err != nil {
		return "", err
	}
	stepSrc, err := os.ReadFile(stepPath)
	if err != nil {
		return "", err
	}
	// step側の表記をfinal側に揃える(importパスと、コメント中の実行パス)。
	normalized := strings.ReplaceAll(string(stepSrc), modulePath+"/"+stepDir, modulePath+"/"+finalDir)
	normalized = strings.ReplaceAll(normalized, "./"+stepDir, "./"+finalDir)

	stepDecls, err := declRanges(stepPath, normalized)
	if err != nil {
		return "", err
	}
	finalDecls, err := declRanges(finalPath, string(finalSrc))
	if err != nil {
		return "", err
	}

	// 穴のある宣言を特定し、両側から伏せる。
	holes := map[string]bool{}
	var stepMask, finalMask []span
	for _, d := range stepDecls {
		switch {
		case d.isImport:
			stepMask = append(stepMask, d.span)
		case strings.Contains(normalized[d.start:d.end], marker):
			holes[d.key] = true
			stepMask = append(stepMask, d.span)
		}
	}
	for _, d := range finalDecls {
		if d.isImport || holes[d.key] {
			finalMask = append(finalMask, d.span)
			delete(holes, d.key)
		}
	}
	// step側にしかない穴 = final に対応する宣言が無い。
	for key := range holes {
		return "", fmt.Errorf("穴のある宣言 %q に対応するものが %s にありません", key, finalPath)
	}

	return firstDiff(mask(string(finalSrc), finalMask), mask(normalized, stepMask)), nil
}

type span struct{ start, end int }

type declInfo struct {
	span
	key      string
	isImport bool
}

func declRanges(path, src string) ([]declInfo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var infos []declInfo
	for _, decl := range file.Decls {
		start := decl.Pos()
		isImport := false
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
		case *ast.GenDecl:
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			isImport = d.Tok == token.IMPORT
		}
		infos = append(infos, declInfo{
			span:     span{fset.Position(start).Offset, fset.Position(decl.End()).Offset},
			key:      declKey(decl),
			isImport: isImport,
		})
	}
	return infos, nil
}

// declKey は宣言を両ファイル間で対応付けるための識別子。
func declKey(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		recv := ""
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv = typeName(d.Recv.List[0].Type) + "."
		}
		return "func " + recv + d.Name.Name
	case *ast.GenDecl:
		var names []string
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, s.Name.Name)
			case *ast.ValueSpec:
				for _, n := range s.Names {
					names = append(names, n.Name)
				}
			case *ast.ImportSpec:
				names = append(names, s.Path.Value)
			}
		}
		return d.Tok.String() + " " + strings.Join(names, ",")
	}
	return "unknown"
}

func typeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + typeName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

// mask は指定範囲を1行の目印に潰す。
//
// 元の行数は保たない。穴やimportは両側で長さが違う(穴の説明コメントの分だけ
// step側が長い、穴で不要になったimportがfinal側だけにある)ため、行数を
// 保つと以降の行が丸ごとずれて、無関係な差分の山になる。
func mask(src string, spans []span) string {
	slices.SortFunc(spans, func(a, b span) int { return cmp.Compare(a.start, b.start) })
	var b bytes.Buffer
	prev := 0
	for _, s := range spans {
		if s.start < prev {
			continue
		}
		b.WriteString(src[prev:s.start])
		b.WriteString("<<<比較対象外(穴 or import)>>>")
		prev = s.end
	}
	b.WriteString(src[prev:])
	return b.String()
}

// firstDiff は最初に食い違う行を文脈つきで返す。
// 行番号は伏せ字適用後のものなので、元ファイルの行番号とは一致しない。
func firstDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		if lineAt(wantLines, i) == lineAt(gotLines, i) {
			continue
		}
		var b strings.Builder
		b.WriteString("      最初の食い違い:\n")
		for j := max(0, i-2); j < i; j++ {
			fmt.Fprintf(&b, "        共通 : %s\n", lineAt(wantLines, j))
		}
		fmt.Fprintf(&b, "        final: %s\n", lineAt(wantLines, i))
		fmt.Fprintf(&b, "        step : %s\n", lineAt(gotLines, i))
		return b.String()
	}
	return ""
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "(行なし)"
}
