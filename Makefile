.PHONY: run test check build

run:
	go run ./cmd/gogif

test:
	go test ./...

check:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
	go vet ./...
	go test -race ./...

build:
	mkdir -p bin
	go build -o bin/gogif ./cmd/gogif
