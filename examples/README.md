# SDK examples

One program per billing use case, written only with the SDK's public API (`client.<Service>.<Method>`, `infi.Paginate`, `*infi.Error`). They import the local package (`github.com/Be-Infinitum/infi-go`).

## How to run

```sh
INFI_SECRET_KEY=sk_test_... go run ./examples/01-billing-as-code
INFI_SECRET_KEY=sk_test_... go run ./examples/run-all   # all of them, with ✓/✗ at the end
```

- `INFI_SECRET_KEY` (required): an `sk_test_` key from a sandbox account that has an owner. The anonymous account (`/public/v1/claimables`) was removed in ADR 0076. Examples 01 and 05 apply config documents to the account: use an account dedicated to the examples. They were written for a clean account on each run; running again on the same account has not been verified.
- `INFI_API_URL` (optional): without it, the SDK uses `https://api-sandbox.beinfi.com` for `sk_test_`. For the local backend, `INFI_API_URL=http://127.0.0.1:8088`.
- Types: `go build ./...` compiles `examples/` together with the package.

## What each program shows

| Program | Demonstrates |
|---|---|
| `01-billing-as-code` | A document with a subscription, a usage-based product and a prepaid one with a grant; plan, apply with `WithIfMatch(planHash)`, pull; apply with a stale hash gives 412, without If-Match gives 428 |
| `02-one-time-link` | One-time product: draft, preview with `DraftHash`, link draft, publish (428 without If-Match) and the link URL |
| `03-subscription-link` | Same flow for a monthly subscription, plus the 412 when the product changes after the preview |
| `04-usage-based` | Usage-based product, customer by productKey, events with a repeated `EventId` counting once, consumption per meter |
| `05-prepaid-credits` | Prepaid declared in config, a credit of 1000 to the wallet, consumption of 200 and waiting for the worker's debit (up to 3 min) |
| `06-customer-session` | Portal session: create, verify, revoke and verify again (401) |
| `07-reconciliation` | `Paginate` sobre `Customers.List` (25 customers, limit 10) e `Payments.List` |
| `08-checkout-delivery-webhook` | Selling to a buyer: published product, delivery by link, subscribed webhook, checkout with the hosted URL and the read that waits for the payment |

`examples/internal/setup` has the helpers (client from `INFI_SECRET_KEY`, printed steps, `ExpectStatus`) and `examples/run-all` runs them all in sequence.

## Program to SDK operations

| Program | Operations |
|---|---|
| 01 | `Config.Plan`, `Config.Apply`, `Config.Pull` |
| 02 | `Products.SaveDraft`, `Products.GetDraft`, `Links.SaveDraft`, `Links.PublishDraft` |
| 03 | `Products.SaveDraft`, `Products.GetDraft`, `Links.SaveDraft`, `Links.GetDraft`, `Links.PublishDraft` |
| 04 | `Products.SaveDraft`, `Products.PublishDraft`, `Config.Pull`, `Customers.Create`, `Usage.Record`, `Usage.Get` |
| 05 | `Config.Plan`, `Config.Apply`, `Customers.Create`, `Customers.CreditWallet`, `Customers.GetWallet`, `Usage.Record` |
| 06 | `Products.SaveDraft`, `Products.PublishDraft`, `Customers.Create`, `CustomerSessions.Create`, `CustomerSessions.Verify`, `CustomerSessions.Revoke` |
| 07 | `Products.SaveDraft`, `Products.PublishDraft`, `Customers.Create`, `Customers.List` and `Payments.List` via `Paginate` |
| 08 | `Products.SaveDraft`, `Products.PublishDraft`, `Products.SetDeliverable`, `Products.GetDeliverable`, `Webhooks.Subscribe`, `Webhooks.List`, `Checkouts.Create`, `Checkouts.Get` |
