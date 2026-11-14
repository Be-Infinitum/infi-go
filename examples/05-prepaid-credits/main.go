// Prepaid: product with a grant and per_unit price, wallet credit, consumption and debit by the worker.
package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	infi "github.com/Be-Infinitum/infi-go"
	"github.com/Be-Infinitum/infi-go/examples/internal/setup"
)

func main() { setup.Run(run) }

func run(ctx context.Context, client *infi.Client) error {
	s := setup.Uniq()

	setup.Step("Declare the prepaid product (billing as code)")
	document := infi.CompanyDocument{
		SchemaVersion: 1,
		Products: []infi.CompanyProduct{{
			Key:          "credits-" + s,
			Name:         infi.Ptr("AI Credits"),
			Type:         infi.CompanyProductTypeAgent,
			PricingModel: infi.CompanyProductPricingModelPrepaid,
			Currency:     infi.Ptr("BRL"),
			BasePrice:    infi.Ptr("0"),
			BillingCycle: infi.Ptr(infi.CompanyProductBillingCycleMonthly),
			Meters:       infi.Ptr([]infi.CompanyMeter{{Key: "tokens", Unit: "token", Aggregation: infi.CompanyMeterAggregationSum}}),
			Prices:       infi.Ptr([]infi.CompanyPrice{{Meter: "tokens", Model: "per_unit", UnitAmount: infi.Ptr("0.01")}}),
			Grants:       infi.Ptr([]infi.CompanyGrant{{Meter: "tokens", Amount: "1000", On: infi.CompanyGrantOnPayment}}),
		}},
	}
	plan, err := client.Config.Plan(ctx, infi.ConfigPlanInput{Body: infi.ConfigPlanJSONRequestBody{Document: document}})
	if err != nil {
		return err
	}
	applied, err := client.Config.Apply(ctx,
		infi.ConfigApplyInput{Body: infi.ConfigApplyJSONRequestBody{Document: document}},
		infi.WithIfMatch(plan.PlanHash))
	if err != nil {
		return err
	}
	setup.Show("revision", fmt.Sprintf("#%d %s", applied.Revision.Number, applied.Revision.Status))

	productKey := document.Products[0].Key
	const meter = "tokens"
	setup.Show("productKey / meter", productKey+" / "+meter)

	setup.Step("Create the customer")
	externalID := "user_" + setup.Uniq()
	customer, err := client.Customers.Create(ctx, infi.CustomersCreateInput{Body: infi.CustomersCreateJSONRequestBody{
		ProductKey: infi.Ptr(productKey), ExternalId: externalID,
	}})
	if err != nil {
		return err
	}
	walletID := customer.EnrollmentId
	setup.Show("enrollmentId (wallet id)", walletID)

	balanceOf := func() (string, error) {
		w, err := client.Customers.GetWallet(ctx, infi.CustomersGetWalletInput{
			Path:  infi.CustomersGetWalletPath{EnrollmentId: walletID},
			Query: infi.CustomersGetWalletQuery{Meter: infi.Ptr(meter)},
		})
		if err != nil {
			return "", err
		}
		// The response is a oneOf: a single balance (with meter) or the list of balances.
		if one, err := w.AsWalletBalance(); err == nil && one.Balance != "" {
			return one.Balance, nil
		}
		many, err := w.AsWalletBalances()
		if err != nil {
			return "", err
		}
		for _, b := range many.Balances {
			if b.Meter == meter {
				return b.Balance, nil
			}
		}
		return "0", nil
	}

	setup.Step("Credit 1000 to the wallet")
	credited, err := client.Customers.CreditWallet(ctx, infi.CustomersCreditWalletInput{
		Path: infi.CustomersCreditWalletPath{EnrollmentId: walletID},
		Body: infi.WalletMutationRequest{Meter: meter, Amount: "1000", Reason: infi.Ptr("starter pack")},
	})
	if err != nil {
		return err
	}
	setup.Show("wallet", credited)
	before, err := balanceOf()
	if err != nil {
		return err
	}
	setup.Show("balance before", before)

	setup.Step("Record consumption of 200")
	rec, err := client.Usage.Record(ctx, infi.UsageRecordInput{Body: infi.UsageRecordJSONRequestBody{
		ProductKey: infi.Ptr(productKey), CustomerId: externalID, Meter: meter, Value: infi.Ptr("200"), EventId: "usage-" + setup.Uniq(),
	}})
	if err != nil {
		return err
	}
	setup.Show("event", rec)

	setup.Step("Wait for the worker to debit (reading the balance every 10s, up to 3 min)")
	deadline := time.Now().Add(3 * time.Minute)
	after, err := balanceOf()
	if err != nil {
		return err
	}
	for after == before && time.Now().Before(deadline) {
		if err := setup.Sleep(ctx, 10*time.Second); err != nil {
			return err
		}
		if after, err = balanceOf(); err != nil {
			return err
		}
		fmt.Printf("   ... balance now: %s\n", after)
	}
	setup.Show("balance before", before)
	setup.Show("balance after", after)
	if after == before {
		return fmt.Errorf("the balance did not drop in 3 minutes: the worker did not debit the prepaid product's wallet")
	}
	b, _ := strconv.ParseFloat(before, 64)
	a, _ := strconv.ParseFloat(after, 64)
	fmt.Printf("   ✓ debited: %s\n", strconv.FormatFloat(b-a, 'f', -1, 64))
	return nil
}
