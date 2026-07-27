#!/usr/bin/env bash
#
# 教材としての不変条件を検査する。通常の build / vet / test では
# 検出できない「教材が壊れている」状態を捕まえるのが目的である。
#
#   1. 構造: 各stepに README.md と hints.md があり、テストを持つ
#      パッケージにはゲート(step_gate_test.go)がある
#   2. 穴あきの赤: 各stepは配布状態でビルドと vet が通り、かつ
#      検証テストは失敗する。テストが通ってしまうstepは課題が
#      機能していない(穴を埋めなくても正解になっている)
#   3. step 10 = final: step 10 の解答は final そのものなので、
#      穴のあるファイルを除いて両者はimportパス以外同一であること
#
# 3が効くのは、final にバグ修正を入れて steps に反映し忘れたときである。
# 実際にカスタムコマンドのシンボリックリンク脱出を修正した際、
# この非対称が起こりうることが分かったので検査に加えた。
#
# 使い方: リポジトリのルートで `make check-steps`
set -uo pipefail

cd "$(dirname "$0")/.."

readonly FINAL_DIR="final"
readonly LAST_STEP_DIR="steps/10-streaming-hooks"
readonly MODULE="github.com/toshi0607/go-coding-agent-handson"

failed=0

fail() {
	echo "  ✗ $*"
	failed=1
}

ok() {
	echo "  ✓ $*"
}

# --- 1. 構造 ---
echo "[1/3] 各stepの構造"
for dir in steps/*/; do
	step=$(basename "$dir")
	for doc in README.md hints.md; do
		if [ ! -f "$dir$doc" ]; then
			fail "$step: $doc がありません"
		fi
	done

	# テストを持つパッケージにはゲートが要る。無いと、そのstepの
	# テストが通常の `go test ./...` で走ってしまい、穴あきのstepが
	# リポジトリ全体のテストを失敗させる。
	while IFS= read -r pkg; do
		if [ ! -f "$pkg/step_gate_test.go" ]; then
			fail "$step: $pkg に step_gate_test.go(テストゲート)がありません"
		fi
	done < <(find "$dir" -name '*_test.go' -not -name 'step_gate_test.go' -exec dirname {} \; | sort -u)
done
[ "$failed" -eq 0 ] && ok "README / hints / テストゲートが揃っています"

# --- 2. 穴あきの赤 ---
echo "[2/3] 配布状態(穴あき)の各step"
for dir in steps/*/; do
	step=$(basename "$dir")
	num=${step%%-*}

	# 穴あきでもコンパイルは通ること。学習者が最初に見るのが
	# コンパイルエラーの山では、どこから手を付けるか分からない。
	if ! go build "./$dir..." > /dev/null 2>&1; then
		fail "$step: 穴あき状態でビルドが通りません"
		continue
	fi
	if ! go vet "./$dir..." > /dev/null 2>&1; then
		fail "$step: 穴あき状態で go vet が通りません"
		continue
	fi

	# 検証テストは失敗すること(= 課題が機能している)。
	if AGENT_STEP_CHECK=1 go test -count=1 "./$dir..." > /dev/null 2>&1; then
		fail "$step: 穴あき状態で検証テストが通ってしまいます(課題が機能していません)"
	else
		ok "$step: ビルド・vetは通り、検証テストは失敗(想定どおり)"
	fi
done

# --- 3. step 10 = final ---
echo "[3/3] step 10 と final の一致(穴のあるファイルを除く)"
mismatch=0
while IFS= read -r final_file; do
	rel=${final_file#"$FINAL_DIR/"}
	step_file="$LAST_STEP_DIR/$rel"

	if [ ! -f "$step_file" ]; then
		fail "step 10 に $rel がありません(final にはあります)"
		mismatch=1
		continue
	fi
	# 穴のあるファイルは当然中身が違う。
	if grep -q 'TODO(step10' "$step_file"; then
		continue
	fi
	if ! diff -q <(sed "s|$MODULE/steps/10-streaming-hooks|$MODULE/$FINAL_DIR|g; s|\./steps/10-streaming-hooks|./$FINAL_DIR|g" "$step_file") "$final_file" > /dev/null; then
		fail "$rel が final と食い違っています(final の修正を step 10 に反映し忘れていませんか)"
		echo "      diff: sed 's|steps/10-streaming-hooks|final|g' $step_file | diff - $final_file"
		mismatch=1
	fi
done < <(find "$FINAL_DIR" -name '*.go' | sort)

# 逆方向(step 10 にあって final に無いファイル)。ゲートは step 側だけにある。
while IFS= read -r step_file; do
	rel=${step_file#"$LAST_STEP_DIR/"}
	[ "$(basename "$rel")" = "step_gate_test.go" ] && continue
	if [ ! -f "$FINAL_DIR/$rel" ]; then
		fail "final に $rel がありません(step 10 にはあります)"
		mismatch=1
	fi
done < <(find "$LAST_STEP_DIR" -name '*.go' | sort)

[ "$mismatch" -eq 0 ] && ok "step 10 は final と一致しています(穴を除く)"

if [ "$failed" -ne 0 ]; then
	echo
	echo "教材の不変条件が壊れています。"
	exit 1
fi
echo
echo "教材の不変条件はすべて満たされています。"
