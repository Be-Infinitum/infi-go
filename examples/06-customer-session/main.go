// Customer session (portal): create, verify, revoke, verify again (fails).
package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	infi "github.com/Be-Infinitum/infi-go"
	"github.com/Be-Infinitum/infi-go/examples/internal/setup"
)

func main() { setup.Run(run) }

func run(ctx context.Context, client *infi.Client) error {
	setup.Step("Create a product and a customer")
	draftKey := setup.UUID()
	draft, err := client.Products.SaveDraft(ctx, infi.ProductsSaveDraftInput{
		Path: infi.ProductsSaveDraftPath{DraftKey: draftKey},
		Body: infi.ProductOfferValues{
			Name:         "Portal Plan",
			PricingModel: infi.ProductOfferValuesPricingModelSubscription,
			Currency:     "BRL",
			BasePrice:    infi.Ptr("19.90"),
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
	externalID := "user_" + setup.Uniq()
	customer, err := client.Customers.Create(ctx, infi.CustomersCreateInput{Body: infi.CustomersCreateJSONRequestBody{
		ProductId: infi.Ptr(product.Product.Id), ExternalId: externalID, Name: infi.Ptr("Bea"), Email: infi.Ptr("bea@example.com"),
	}})
	if err != nil {
		return err
	}
	setup.Show("customer", customer)

	setup.Step("Create the session for the externalId")
	session, err := client.CustomerSessions.Create(ctx, infi.CustomerSessionsCreateInput{
		Body: infi.CustomerSessionsCreateJSONRequestBody{ExternalId: externalID},
	})
	if err != nil {
		return err
	}
	token := session.Token
	if len(token) > 12 {
		token = token[:12]
	}
	setup.Show("token", token+"…")
	setup.Show("scope / origin / expires", fmt.Sprintf("%s / %s / %s", session.Scope, session.Origin, session.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000Z")))

	verify := func() (*infi.CustomerSessionsVerify200JSONResponseBody, error) {
		return client.CustomerSessions.Verify(ctx, infi.CustomerSessionsVerifyInput{
			Headers: infi.CustomerSessionsVerifyHeaders{XBuyerToken: session.Token},
		})
	}

	setup.Step("Verify the token")
	verified, err := verify()
	if err != nil {
		return err
	}
	setup.Show("buyer", verified.Buyer)
	setup.Show("access", len(verified.Access))
	if !slices.Contains(verified.Buyer.ExternalIds, externalID) {
		return errors.New("the token does not point to the externalId that was created")
	}

	setup.Step("Revoke the token")
	revoked, err := client.CustomerSessions.Revoke(ctx, infi.CustomerSessionsRevokeInput{
		Body: infi.CustomerSessionsRevokeJSONRequestBody{Token: infi.Ptr(session.Token)},
	})
	if err != nil {
		return err
	}
	setup.Show("revoked", revoked.Revoked)

	setup.Step("Verify again (should fail)")
	_, err = verify()
	e, err := setup.ExpectStatus(401, err)
	if err != nil {
		return err
	}
	setup.Show("infi.Error", fmt.Sprintf("status=%d code=%s", e.Status, e.Code))
	return nil
}
