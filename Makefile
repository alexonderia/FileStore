GO ?= go
VERSION ?= dev
LDFLAGS := -X main.version=$(VERSION)

.PHONY: fmt-check vet test integration build verify openapi-lint

fmt-check:
	@unformatted="$$(gofmt -l $$(find . -type f -name '*.go' -not -path './.git/*'))"; test -z "$$unformatted" || (echo "Go files need formatting"; echo "$$unformatted"; exit 1)

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -coverprofile=coverage.out ./...

integration:
	FILESTORE_TEST_DATABASE_URL='postgres://filestore:filestore-local@localhost:5432/filestore?sslmode=disable' FILESTORE_TEST_S3_ENDPOINT='http://localhost:8333' $(GO) test -count=1 ./tests/integration ./tests/e2e

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/filestore-api ./cmd/filestore-api
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/filestore ./cmd/filestore

openapi-lint:
	npx --yes @redocly/cli@1.34.3 lint openapi/openapi.yaml

verify: fmt-check vet test build openapi-lint
