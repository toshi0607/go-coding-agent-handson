package tools

// このパッケージのテストは、リポジトリのルートで
//
//	make check STEP=07
//
// を実行したときだけ走ります。環境変数 AGENT_STEP_CHECK が未設定の場合は
// 何もせずに終了します。穴あき(未実装)のstepがリポジトリ全体の
// go test ./... を失敗させないためのゲートです。
//
// このファイルは課題の対象ではありません。編集不要です。

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("AGENT_STEP_CHECK") == "" {
		fmt.Println("skip: steps のテストは make check STEP=NN で実行します")
		os.Exit(0)
	}
	os.Exit(m.Run())
}
