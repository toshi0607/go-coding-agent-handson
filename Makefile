# make check で全体の健全性を確認する(APIキー不要)
.PHONY: check build vet test

check: build vet test

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

# steps/ 追加後は `make check STEP=01` で該当stepの検証テストを実行できるようにする予定
