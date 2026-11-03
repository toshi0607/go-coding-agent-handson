package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// 通常の行と、改行で終わらない最後の行を、ReadString と同じ順で返すこと。
func TestLineReaderReadsLines(t *testing.T) {
	lr := newLineReader(strings.NewReader("1行目\n最後の行"))

	line, err := lr.ReadLine(t.Context())
	if line != "1行目\n" || err != nil {
		t.Fatalf("1行目: %q, %v", line, err)
	}
	line, err = lr.ReadLine(t.Context())
	if line != "最後の行" || !errors.Is(err, io.EOF) {
		t.Fatalf("最後の行: %q, %v", line, err)
	}
	if _, err := lr.ReadLine(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatalf("読み終えた後は io.EOF のはず: %v", err)
	}
}

// 入力が来ないまま ctx がキャンセルされたら、ReadLine が戻ること。
// これが入力待ちの間の Ctrl-C で終了できる理由である。
func TestLineReaderReturnsOnCancel(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	lr := newLineReader(r)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := lr.ReadLine(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("キャンセルで戻るべき: %v", err)
	}
}
