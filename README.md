# infi-go

Infi Go SDK, generated from the public API contract (`api/openapi.sdk.json`, a copy of the monorepo's `backend/api/openapi.sdk.json`; ADR 0055).

The contract arrives by copy: when `backend/api/openapi.sdk.*` changes in the monorepo, copy it here and regenerate. With the monorepo next to this repo: `INFI_BACKEND=<path>/backend make sync-contract` (copies the contract and runs `go generate ./...`).

```sh
go get github.com/Be-Infinitum/infi-go
```

```go
import infi "github.com/Be-Infinitum/infi-go"

client, err := infi.New(os.Getenv("INFI_SECRET_KEY")) // sk_test_ -> sandbox, sk_live_ -> production
```

Every call takes a `context.Context` and returns `(*Response, error)`. Parameters live in one struct per operation, with `Path`, `Query`, `Headers` and `Body` (types generated from the contract). Optional fields are pointers; `infi.Ptr(v)` creates one.

- payments: `client.Payments.List(ctx, infi.PaymentsListInput{Query: infi.PaymentsListQuery{Status: infi.Ptr("confirmed")}})`; `client.Payments.Get(ctx, infi.PaymentsGetInput{Path: infi.PaymentsGetPath{PaymentId: id}})`
- usage: `client.Usage.Record(ctx, infi.UsageRecordInput{Body: infi.UsageRecordJSONRequestBody{CustomerId: customerID, Meter: meter, EventId: eventID, Value: infi.Ptr("1")}})`; `client.Usage.Get(ctx, infi.UsageGetInput{Query: infi.UsageGetQuery{EnrollmentId: id}})`
- customers: `client.Customers.GetWallet(ctx, infi.CustomersGetWalletInput{Path: infi.CustomersGetWalletPath{EnrollmentId: id}})`; `client.Customers.CreditWallet(...)`
- customerSessions: `client.CustomerSessions.Create(ctx, infi.CustomerSessionsCreateInput{Body: infi.CustomerSessionsCreateJSONRequestBody{ExternalId: externalID}})`
- products: `saved, err := client.Products.SaveDraft(ctx, infi.ProductsSaveDraftInput{Path: infi.ProductsSaveDraftPath{DraftKey: key}, Body: offer})` and `client.Products.PublishDraft(...)`
- links: `client.Links.PublishDraft(ctx, infi.LinksPublishDraftInput{Path: infi.LinksPublishDraftPath{DraftKey: key}}, infi.WithIfMatch(draftHash))`
- checkouts: `c, err := client.Checkouts.Create(ctx, infi.CheckoutsCreateInput{Body: infi.CheckoutsCreateJSONRequestBody{ProductId: productID, Customer: infi.CheckoutCreateRequest_Customer{ExternalId: buyer}}})`: send the buyer to `c.Url`; `client.Checkouts.Get(ctx, infi.CheckoutsGetInput{Path: infi.CheckoutsGetPath{CheckoutId: c.Id}})` reads `Status` and the payments
- webhooks: `client.Webhooks.Subscribe(ctx, infi.WebhooksSubscribeInput{Body: infi.WebhookSubscribeRequest{Url: url, Events: []string{"payment.confirmed"}}})` (in live mode it returns `ApprovalUrl`); `client.Webhooks.List(ctx)`
- delivery: `client.Products.SetDeliverable(ctx, infi.ProductsSetDeliverableInput{Path: infi.ProductsSetDeliverablePath{ProductId: id}, Body: infi.PutDeliverableRequest{Kind: infi.PutDeliverableRequestKindLink, Url: infi.Ptr(url)}})`
- config: `plan, err := client.Config.Plan(ctx, infi.ConfigPlanInput{Body: infi.ConfigPlanJSONRequestBody{Document: doc}})` and `client.Config.Apply(ctx, infi.ConfigApplyInput{Body: infi.ConfigApplyJSONRequestBody{Document: doc}}, infi.WithIfMatch(plan.PlanHash))`

List endpoints paginate by cursor, using `iter.Seq2` (Go 1.23+):

```go
for p, err := range infi.Paginate(func(cursor *string) ([]infi.Payment, *string, error) {
	l, err := client.Payments.List(ctx, infi.PaymentsListInput{Query: infi.PaymentsListQuery{Limit: infi.Ptr(50), Cursor: cursor}})
	if err != nil {
		return nil, nil, err
	}
	return l.Data, l.NextCursor, nil
}) {
	if err != nil {
		return err
	}
	_ = p
}
```

Webhooks: `infi.VerifyWebhook(infi.WebhookRequestFrom(r.Header, rawBody), secret)` with the `X-Webhook-Id`, `X-Webhook-Timestamp` and `X-Webhook-Signature` headers and the raw body (accepts several secrets, for rotation; 5-minute tolerance, adjustable with `VerifyWebhookWithOptions`).

Client options: `infi.WithAPIURL(url)` (overrides the URL derived from the key), `infi.WithHTTPClient(c)` and `infi.WithMaxAttempts(n)`.

## Idempotency and If-Match

- Every mutation (POST/PUT/PATCH/DELETE) carries an `Idempotency-Key`. If you don't pass `infi.WithIdempotencyKey(k)`, the SDK generates a UUID v4 and reuses the same key on retries (network error, 429, 5xx; up to 3 attempts, waits of 250 ms, 1 s and 4 s, or the server's `Retry-After`). 4xx is never retried, and cancelling the `context` stops everything.
- Publishing and applying require `infi.WithIfMatch(hash)` (the hash of the preview or the plan). If the state changed, the server answers 412 and `(*infi.Error).Current()` returns the current state so you can show the preview again.
- `infi.WithChannel(infi.ChannelCLI)` (panel, mcp, cli or api) sends `X-Infi-Channel`.
- Errors become `*infi.Error` (`Status`, `Code`, `Message`, `TracerID`, `Details`, `Body`); use `errors.As`. A network error has `Status` 0 and `Code` `network_error`.

## This package is generated

Edit the contract (in the monorepo), not the code. `go generate ./...` regenerates `types.gen.go` (types, by [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) v2.8.2, models only) and `resources.gen.go` (one service per tag and one method per operation, by `internal/cmd/genfacade`). Only `client.go`, `transport.go`, `errors.go`, `paginate.go` and `webhook.go` are handwritten. The generated types import `github.com/oapi-codegen/runtime` (the only dependency, used by the `oneOf` unions).

CI (`.github/workflows/ci.yml`) fails if:

- `make check-generated` changes anything (the generated code doesn't match `api/openapi.sdk.json`);
- `go vet`, `go test` or `go build` break;
- any example in `examples/` fails against `api-sandbox.beinfi.com`, using the key from the `INFI_SANDBOX_SECRET_KEY` secret. Each SDK repo uses its own sandbox account: the four CIs run at the same time, and on a shared account they race on its config (412 `plan_changed`) and on its rate limit (429). The `*_test.go` tests only cover the transport; the proof that the SDK talks to the API is the examples.

Publishing is manual: the `release` workflow takes the version (for example `v1.0.0`), reruns the checks, runs the examples, creates and pushes the tag and opens the release on GitHub. Go modules are published by tag; the proxy (`proxy.golang.org`) picks it up on its own.
