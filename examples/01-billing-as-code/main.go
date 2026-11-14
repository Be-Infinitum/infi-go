// Billing as code: declares the company in a document, plans, applies and checks with pull.
package main

import (
	"context"
	"fmt"

	infi "github.com/Be-Infinitum/infi-go"
	"github.com/Be-Infinitum/infi-go/examples/internal/setup"
)

func main() { setup.Run(run) }

func run(ctx context.Context, client *infi.Client) error {
	s := setup.Uniq()

	document := infi.CompanyDocument{
		SchemaVersion: 1,
		Products: []infi.CompanyProduct{
			{
				Key:          "subscription-" + s,
				Name:         infi.Ptr("Pro Plan"),
				Type:         infi.CompanyProductTypeItem,
				PricingModel: infi.CompanyProductPricingModelSubscription,
				Currency:     infi.Ptr("BRL"),
				BasePrice:    infi.Ptr("49.90"),
				BillingCycle: infi.Ptr(infi.CompanyProductBillingCycleMonthly),
			},
			{
				Key:          "usage-" + s,
				Name:         infi.Ptr("Usage-based API"),
				Type:         infi.CompanyProductTypeAgent,
				PricingModel: infi.CompanyProductPricingModelUsage,
				Currency:     infi.Ptr("BRL"),
				Meters:       infi.Ptr([]infi.CompanyMeter{{Key: "requests", Unit: "request", Aggregation: infi.CompanyMeterAggregationCount}}),
				Prices:       infi.Ptr([]infi.CompanyPrice{{Meter: "requests", Model: "per_unit", UnitAmount: infi.Ptr("0.05")}}),
			},
			{
				Key:          "prepaid-" + s,
				Name:         infi.Ptr("AI Credits"),
				Type:         infi.CompanyProductTypeAgent,
				PricingModel: infi.CompanyProductPricingModelPrepaid,
				Currency:     infi.Ptr("BRL"),
				BasePrice:    infi.Ptr("0"),
				BillingCycle: infi.Ptr(infi.CompanyProductBillingCycleMonthly),
				Meters:       infi.Ptr([]infi.CompanyMeter{{Key: "tokens", Unit: "token", Aggregation: infi.CompanyMeterAggregationSum}}),
				Prices:       infi.Ptr([]infi.CompanyPrice{{Meter: "tokens", Model: "per_unit", UnitAmount: infi.Ptr("0.01")}}),
				Grants:       infi.Ptr([]infi.CompanyGrant{{Meter: "tokens", Amount: "1000", On: infi.CompanyGrantOnPayment}}),
			},
		},
	}

	setup.Step("Plan the document (writes nothing)")
	plan, err := client.Config.Plan(ctx, infi.ConfigPlanInput{Body: infi.ConfigPlanJSONRequestBody{Document: document}})
	if err != nil {
		return err
	}
	setup.Show("planHash", plan.PlanHash)
	setup.Show("mode", fmt.Sprintf("%s, hasChanges=%t, blocked=%t", plan.Mode, plan.HasChanges, plan.Blocked))
	for _, c := range plan.Changes {
		fmt.Printf("   - [%s] %s %s: %s\n", c.Action, c.Resource, c.Ref, c.Sentence)
	}

	setup.Step("Apply with If-Match = planHash")
	apply := infi.ConfigApplyInput{Body: infi.ConfigApplyJSONRequestBody{Document: document}}
	applied, err := client.Config.Apply(ctx, apply, infi.WithIfMatch(plan.PlanHash))
	if err != nil {
		return err
	}
	setup.Show("revision", fmt.Sprintf("#%d status=%s mode=%s", applied.Revision.Number, applied.Revision.Status, applied.Revision.Mode))

	setup.Step("Applying again with the stale hash should give 412")
	_, err = client.Config.Apply(ctx, apply, infi.WithIfMatch(plan.PlanHash))
	stale, err := setup.ExpectStatus(412, err)
	if err != nil {
		return err
	}
	setup.Show("infi.Error", fmt.Sprintf("status=%d code=%s message=%q", stale.Status, stale.Code, stale.Message))
	current := "(no current)"
	if m, ok := stale.Current().(map[string]any); ok {
		if h, ok := m["planHash"].(string); ok {
			current = h
		}
	}
	setup.Show("current.planHash (recomputed plan)", current)

	setup.Step("Applying without If-Match should give 428")
	_, err = client.Config.Apply(ctx, apply)
	noMatch, err := setup.ExpectStatus(428, err)
	if err != nil {
		return err
	}
	setup.Show("infi.Error", fmt.Sprintf("status=%d code=%s", noMatch.Status, noMatch.Code))

	setup.Step("Pull the document from the server and check the products")
	pulled, err := client.Config.Pull(ctx)
	if err != nil {
		return err
	}
	for _, p := range pulled.Products {
		fmt.Printf("   - %s (%s) meters=%d prices=%d grants=%d\n", p.Key, p.PricingModel, setup.Len(p.Meters), setup.Len(p.Prices), setup.Len(p.Grants))
	}
	for _, p := range document.Products {
		var got *infi.CompanyProduct
		for i := range pulled.Products {
			if pulled.Products[i].Key == p.Key {
				got = &pulled.Products[i]
			}
		}
		if got == nil {
			return fmt.Errorf("product %s did not come back in the pull", p.Key)
		}
		if got.PricingModel != p.PricingModel {
			return fmt.Errorf("%s: pricingModel %s != %s", p.Key, got.PricingModel, p.PricingModel)
		}
	}
	fmt.Println("   ✓ all 3 products came back")

	setup.Step("Plan again: nothing to change")
	again, err := client.Config.Plan(ctx, infi.ConfigPlanInput{Body: infi.ConfigPlanJSONRequestBody{Document: document}})
	if err != nil {
		return err
	}
	setup.Show("hasChanges", again.HasChanges)
	if again.HasChanges {
		return fmt.Errorf("plan after apply still has changes")
	}
	return nil
}
