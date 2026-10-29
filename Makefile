GO ?= go
.PHONY: fmt lint test test-race build integration snapshot check
fmt:
	gofmt -w cmd internal test
lint:
	$(GO) vet ./...
test:
	$(GO) test ./...
test-race:
	$(GO) test -race ./...
build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/raymond ./cmd/raymond
integration:
	$(GO) test -race ./test/integration/...
snapshot:
	goreleaser release --snapshot --clean
check:
	test -z "$$(gofmt -l cmd internal test)"
	$(MAKE) lint test test-race build
