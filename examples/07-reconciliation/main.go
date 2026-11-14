// Reconciliation: walks whole lists with Paginate (customers and payments).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	infi "github.com/Be-Infinitum/infi-go"
	"github.com/Be-Infinitum/infi-go/examples/internal/setup"
)

func main() { setup.Run(run) }

func run(ctx context.Context, client *infi.Client) error {
	setup.Step("Create a product and ~25 customers")
	draftKey := setup.UUID()
	draft, err := client.Products.SaveDraft(ctx, infi.ProductsSaveDraftInput{
		Path: infi.ProductsSaveDraftPath{DraftKey: draftKey},
		Body: infi.ProductOfferValues{
			Name:         "Reconciliation Product",
			PricingModel: infi.ProductOfferValuesPricingModelSubscription,
			Currency:     "BRL",
			BasePrice:    infi.Ptr("10.00"),
			BillingCycle: infi.Ptr(infi.ProductOfferValuesBillingCycleMonthly),
		},
	})
	if err != nil {
		return err
	}
	product, err := client.Products.PublishDraft(ctx,
		infi.ProductsPublishDraftInput{Path: infi.ProductsPublishDraftPath{DraftKey: draftKey}},
		infi.WithIfMatch(draft.DraftHash))
	if err != nil {
		return err
	}
	s := setup.Uniq()
	const total = 25
	for i := 1; i <= total; i++ {
		if _, err := client.Customers.Create(ctx, infi.CustomersCreateInput{Body: infi.CustomersCreateJSONRequestBody{
			ProductId: infi.Ptr(product.Product.Id), ExternalId: fmt.Sprintf("cus_%s_%02d", s, i),
		}}); err != nil {
			return err
		}
	}
	setup.Show("created", total)

	setup.Step("Paginate customers.List with limit 10")
	listCustomers := func(cursor *string) ([]infi.Customer, *string, error) {
		l, err := client.Customers.List(ctx, infi.CustomersListInput{Query: infi.CustomersListQuery{Limit: infi.Ptr(10), Cursor: cursor}})
		if err != nil {
			return nil, nil, err
		}
		return l.Data, l.NextCursor, nil
	}
	// The sandbox account may already have customers from other runs: the proof is that
	// pagination repeats nobody and that all 25 just created show up.
	mine := "cus_" + s + "_"
	seen := 0
	ids := map[string]bool{}
	ours := map[string]bool{}
	for c, err := range infi.Paginate(listCustomers) {
		if err != nil {
			return err
		}
		seen++
		ids[c.Id] = true
		if strings.HasPrefix(c.ExternalId, mine) {
			ours[c.Id] = true
		}
	}
	first, err := client.Customers.List(ctx, infi.CustomersListInput{Query: infi.CustomersListQuery{Limit: infi.Ptr(10)}})
	if err != nil {
		return err
	}
	next := "no"
	if first.NextCursor != nil {
		next = "yes"
	}
	setup.Show("items seen", seen)
	setup.Show("distinct ids", len(ids))
	setup.Show("created now", len(ours))
	setup.Show("first page", fmt.Sprintf("%d items, nextCursor=%s, summary=%s", len(first.Data), next, jsonOf(first.Summary)))
	if len(ids) != seen {
		return fmt.Errorf("pagination repeated customers: %d items, %d distinct", seen, len(ids))
	}
	if len(ours) != total {
		return fmt.Errorf("expected the %d customers created now, saw %d", total, len(ours))
	}
	fmt.Printf("   ✓ %d customers in %d pages, no repeats, including the %d created now\n", seen, (seen+9)/10, total)

	setup.Step("Paginate payments.List")
	payments := 0
	for _, err := range infi.Paginate(func(cursor *string) ([]infi.Payment, *string, error) {
		l, err := client.Payments.List(ctx, infi.PaymentsListInput{Query: infi.PaymentsListQuery{Limit: infi.Ptr(10), Cursor: cursor}})
		if err != nil {
			return nil, nil, err
		}
		return l.Data, l.NextCursor, nil
	}) {
		if err != nil {
			return err
		}
		payments++
	}
	setup.Show("payments", payments)
	if payments == 0 {
		fmt.Println("   (empty: in the local sandbox no payer has paid a link; pagination runs the same, there are just no items)")
	}
	return nil
}

func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
