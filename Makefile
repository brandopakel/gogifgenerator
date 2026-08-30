.PHONY: run test check build builds

run:
	@set -a; if [ -f .env ]; then . ./.env; fi; set +a; exec go run ./cmd/gogif

test:
	go test ./...

check:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
	go vet ./...
	go test -race ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/gogif ./cmd/gogif

builds:
	sh scripts/build-release.sh
