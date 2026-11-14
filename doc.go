// Package infi is the Infi Go SDK, generated from the public API contract
// (api/openapi.sdk.json; ADR 0055 in the monorepo).
//
// Generated (never edit by hand): types.gen.go and resources.gen.go, by
// `go generate ./...`. Handwritten (thin layer, ADR 0055 D3): client.go,
// transport.go, errors.go, paginate.go, webhook.go.
//
//	client, err := infi.New(os.Getenv("INFI_SECRET_KEY")) // sk_test_ -> sandbox, sk_live_ -> production
//	c, err := client.Checkouts.Create(ctx, infi.CheckoutsCreateInput{Body: ...})
package infi
