.PHONY: run worker test check build builds worker-windows

run:
	@set -a; if [ -f .env ]; then . ./.env; fi; set +a; exec go run ./cmd/gogif

worker:
	@set -a; if [ -f .env.worker ]; then . ./.env.worker; fi; set +a; exec go run ./cmd/gogif-scene-worker

test:
	go test ./...

check:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
	go vet ./...
	go test -race ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/gogif ./cmd/gogif
	CGO_ENABLED=0 go build -trimpath -o bin/gogif-scene-worker ./cmd/gogif-scene-worker

worker-windows:
	mkdir -p bin
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o bin/gogif-scene-worker.exe ./cmd/gogif-scene-worker

builds:
	sh scripts/build-release.sh
