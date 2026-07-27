# 使い方(いずれもAPIキー不要):
#
#   make check           リポジトリ全体の健全性確認(build + vet + test)
#   make check STEP=01   steps/01-*/ の検証テストを実行(rustlings方式)
#   make check-steps     教材としての不変条件を検査(主にCI用)
#
# STEP指定時は該当stepだけを build / vet した上で、環境変数
# AGENT_STEP_CHECK を立てて検証テストを走らせる。steps/ のテストは
# このゲートが立っているときだけ実行される(穴あきのstepが
# リポジトリ全体の go test ./... を失敗させないため)。
.PHONY: check check-steps build vet test

STEPDIR = $(wildcard steps/$(STEP)-*)

check:
ifdef STEP
	@test -n "$(STEPDIR)" || { echo "steps/$(STEP)-* が見つかりません(例: make check STEP=01)"; exit 1; }
	go build ./$(STEPDIR)/...
	go vet ./$(STEPDIR)/...
	AGENT_STEP_CHECK=1 go test ./$(STEPDIR)/...
else
	$(MAKE) build vet test
endif

# 教材が壊れていないか(穴あきstepが赤であること、step 10 = final など)を
# 検査する。全stepのテストを走らせるので1分ほどかかる。
check-steps:
	./scripts/check-steps.sh

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...
