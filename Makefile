.PHONY: test test-integration test-all vet lint run generate

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

test-all: test test-integration

vet:
	go vet ./...
	go vet -tags=integration ./...

lint:
	golangci-lint run --timeout=5m

run:
	go run ./cmd/inventoryd

generate:
	buf dep update
	buf lint
	buf generate
