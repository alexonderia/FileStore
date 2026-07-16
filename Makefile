GO ?= go
VERSION ?= dev
LDFLAGS := -X main.version=$(VERSION)

.PHONY: fmt-check vet test build verify openapi-lint

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Go files need formatting" && gofmt -l . && exit 1)

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -coverprofile=coverage.out ./...

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/filestore-api ./cmd/filestore-api
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/filestore ./cmd/filestore

openapi-lint:
	npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml

verify: fmt-check vet test build openapi-lint
