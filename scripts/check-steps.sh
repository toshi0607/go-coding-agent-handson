#!/usr/bin/env bash
#
# 教材としての不変条件を検査する。通常の build / vet / test では
# 検出できない「教材が壊れている」状態を捕まえるのが目的である。
#
#   1. 構造: 各stepに README.md と hints.md があり、テストを持つ
#      パッケージにはゲート(step_gate_test.go)がある
#   2. 穴あきの赤: 各stepは配布状態でビルドと vet が通り、かつ
#      **穴のあるパッケージだけ**が検証テストに失敗する。
#      「失敗しさえすればよい」ではない——fixtureの欠落やタイムアウトで
#      落ちていても合格になってしまい、チェッカー自身が壊れていることに
#      気づけないため、失敗したパッケージの集合と、穴(TODOマーカー)を
#      含むパッケージの集合が一致することまで見る
#   3. step 10 = final: step 10 の解答は final そのものなので、
#      穴のある宣言を除いて両者は一致すること(scripts/sync_check.go)
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

	# 穴(TODOマーカー)を含むパッケージ = 失敗するはずのパッケージ。
	marker="TODO(step$num"
	expected=$(grep -rl "$marker" "$dir" --include='*.go' 2>/dev/null |
		xargs -n1 dirname | sort -u)
	if [ -z "$expected" ]; then
		fail "$step: 穴($marker...)が1つもありません"
		continue
	fi

	output=$(AGENT_STEP_CHECK=1 go test -count=1 "./$dir..." 2>&1)
	if [ $? -eq 0 ]; then
		fail "$step: 穴あき状態で検証テストが通ってしまいます(課題が機能していません)"
		continue
	fi

	# ビルド不能・セットアップ失敗は「課題として正しく失敗している」とは言えない。
	if echo "$output" | grep -q '\[build failed\]\|\[setup failed\]'; then
		fail "$step: テストがビルドできずに失敗しています"
		echo "$output" | grep -m3 '\[build failed\]\|\[setup failed\]' | sed 's/^/      /'
		continue
	fi

	# 実際に失敗したパッケージ(FAIL行のimportパスをディレクトリに直す)。
	actual=$(echo "$output" | awk '/^FAIL\t/ {print $2}' |
		sed "s|^github.com/toshi0607/go-coding-agent-handson/||" | sort -u)

	# 「失敗した == 穴がある」の一致を見る。ズレは両方向とも問題である:
	#   穴があるのに落ちない → その課題は埋めなくても正解になっている
	#   穴がないのに落ちる   → fixture欠落やタイムアウトなど別の理由で
	#                          落ちており、赤の理由が課題ではない
	if [ "$expected" != "$actual" ]; then
		fail "$step: 失敗したパッケージが穴のあるパッケージと一致しません"
		echo "      穴がある: $(echo "$expected" | tr '\n' ' ')" | sed 's/ $//'
		echo "      失敗した: $(echo "$actual" | tr '\n' ' ')" | sed 's/ $//'
		continue
	fi

	ok "$step: ビルド・vetは通り、穴のある $(echo "$expected" | wc -l | tr -d ' ') パッケージだけが失敗(想定どおり)"
done

# --- 3. step 10 = final ---
#
# ファイル単位ではなくトップレベル宣言の単位で比較する。穴のあるファイルを
# 丸ごと除外すると、同じファイルにある他の関数(hooks.go の RunPost など)の
# 食い違いを見逃すためである。詳細は scripts/sync_check.go を参照。
echo "[3/3] step 10 と final の一致(穴のある宣言を除く)"
if ! go run scripts/sync_check.go "$FINAL_DIR" "$LAST_STEP_DIR" 'TODO(step10'; then
	failed=1
fi

if [ "$failed" -ne 0 ]; then
	echo
	echo "教材の不変条件が壊れています。"
	exit 1
fi
echo
echo "教材の不変条件はすべて満たされています。"
