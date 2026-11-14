# Contract -> code (ADR 0055). The *.gen.go files are never edited by hand.
INFI_BACKEND ?= ../infi/backend

.PHONY: generate check-generated sync-contract vet test build examples check

generate:
	go generate ./...

# Fails if the generated code doesn't match api/openapi.sdk.json.
check-generated: generate
	git diff --exit-code -- types.gen.go resources.gen.go
	@test -z "$$(git ls-files --others --exclude-standard -- types.gen.go resources.gen.go)" || (echo "generated file outside git"; exit 1)

# Copies the contract from the monorepo and regenerates.
sync-contract:
	cp $(INFI_BACKEND)/api/openapi.sdk.json $(INFI_BACKEND)/api/openapi.sdk.yaml api/
	$(MAKE) generate

vet:
	go vet ./...

test:
	go test ./...

build:
	go build ./...

# Examples against the real sandbox (needs INFI_SECRET_KEY).
examples:
	go run ./examples/run-all

check: check-generated vet test build
