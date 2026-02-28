.PHONY: build test test-integration test-cover vet lint clean

build:
	CGO_ENABLED=0 go build -o bin/slimnode ./cmd/slimnode
	CGO_ENABLED=0 go build -o bin/slimnode-server ./cmd/slimnode-server

test:
	go test ./... -count=1 -race

test-integration:
	go test -tags=integration -count=1 -race ./tests/integration/

test-cover:
	go test ./internal/... -coverprofile=coverage.out -count=1 && go tool cover -func=coverage.out | tail -1

vet:
	go vet ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/
