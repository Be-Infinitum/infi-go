// Usage-based billing: usage product (draft), customer, events with an idempotent eventId, consumption per meter.
package main

import (
	"context"
	"errors"
	"fmt"

	infi "github.com/Be-Infinitum/infi-go"
	"github.com/Be-Infinitum/infi-go/examples/internal/setup"
)

func main() { setup.Run(run) }

func run(ctx context.Context, client *infi.Client) error {
	setup.Step("Create and publish the usage-based product (draft -> publish)")
	draftKey := setup.UUID()
	draft, err := client.Products.SaveDraft(ctx, infi.ProductsSaveDraftInput{
		Path: infi.ProductsSaveDraftPath{DraftKey: draftKey},
		Body: infi.ProductOfferValues{
			Name:         "Summaries API",
			PricingModel: infi.ProductOfferValuesPricingModelUsage,
			Currency:     "BRL",
			Usage:        &infi.ProductOfferUsage{Unit: "request", UnitAmount: "0.05", Aggregation: infi.ProductOfferUsageAggregationCount},
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
	if product.Product.Key == nil {
		return errors.New("product without key")
	}
	productKey := *product.Product.Key
	setup.Show("productKey", productKey)

	setup.Step("Find the meter key (config.pull)")
	doc, err := client.Config.Pull(ctx)
	if err != nil {
		return err
	}
	meter := ""
	for _, p := range doc.Products {
		if p.Key == productKey && p.Meters != nil && len(*p.Meters) > 0 {
			meter = (*p.Meters)[0].Key
		}
	}
	if meter == "" {
		return errors.New("the pull did not return the usage product's meter")
	}
	setup.Show("meter", meter)

	setup.Step("Create the customer with externalId")
	externalID := "user_" + setup.Uniq()
	customer, err := client.Customers.Create(ctx, infi.CustomersCreateInput{Body: infi.CustomersCreateJSONRequestBody{
		ProductKey: infi.Ptr(productKey), ExternalId: externalID, Name: infi.Ptr("Ann"), Email: infi.Ptr("ann@example.com"),
	}})
	if err != nil {
		return err
	}
	setup.Show("customer", customer)

	setup.Step("Record usage events (and repeat an eventId)")
	record := func(eventID string) (*infi.IngestResult, error) {
		return client.Usage.Record(ctx, infi.UsageRecordInput{Body: infi.UsageRecordJSONRequestBody{
			ProductKey: infi.Ptr(productKey), CustomerId: externalID, Meter: meter, Value: infi.Ptr("1"), EventId: eventID,
		}})
	}
	for _, eventID := range []string{"evt-1", "evt-2", "evt-3"} {
		r, err := record(eventID)
		if err != nil {
			return err
		}
		setup.Show(eventID, r)
	}
	replay, err := record("evt-1")
	if err != nil {
		return err
	}
	setup.Show("evt-1 again", replay)

	setup.Step("Read consumption per meter (expected: 3 events, not 4)")
	usage, err := client.Usage.Get(ctx, infi.UsageGetInput{Query: infi.UsageGetQuery{EnrollmentId: customer.EnrollmentId}})
	if err != nil {
		return err
	}
	var got *infi.MeterUsage
	for i, m := range usage.Meters {
		fmt.Printf("   - %s: total=%s %s, events=%d, amount=%s %s\n", m.Meter, m.TotalValue, m.Unit, m.EventCount, setup.Deref(m.TotalAmount, "-"), setup.Deref(m.Currency, ""))
		if m.Meter == meter {
			got = &usage.Meters[i]
		}
	}
	if got == nil || got.EventCount != 3 {
		count := -1
		if got != nil {
			count = got.EventCount
		}
		return fmt.Errorf("expected 3 events, got %d (a repeated eventId should count once)", count)
	}
	fmt.Println("   ✓ the repeated eventId counted once")
	return nil
}
