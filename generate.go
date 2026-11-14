package infi

// types.gen.go: contract types (oapi-codegen, version pinned below).
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config internal/oapi/config.yaml api/openapi.sdk.json

// resources.gen.go: one service per tag, one method per operation.
//go:generate go run ./internal/cmd/genfacade -in api/openapi.sdk.json -out resources.gen.go
