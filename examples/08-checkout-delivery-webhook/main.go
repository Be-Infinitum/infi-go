// Selling to a buyer: published product -> delivery by URL -> webhook -> checkout -> wait for payment.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	infi "github.com/Be-Infinitum/infi-go"
	"github.com/Be-Infinitum/infi-go/examples/internal/setup"
)

func main() { setup.Run(run) }

func run(ctx context.Context, client *infi.Client) error {
	setup.Step("Publish a one-time product")
	draftKey := setup.UUID()
	saved, err := client.Products.SaveDraft(ctx, infi.ProductsSaveDraftInput{
		Path: infi.ProductsSaveDraftPath{DraftKey: draftKey},
		Body: infi.ProductOfferValues{
			Name:         "PDF Guide",
			PricingModel: infi.ProductOfferValuesPricingModelOneTime,
			Currency:     "BRL",
			BasePrice:    infi.Ptr("19.00"),
		},
	})
	if err != nil {
		return err
	}
	published, err := client.Products.PublishDraft(ctx,
		infi.ProductsPublishDraftInput{Path: infi.ProductsPublishDraftPath{DraftKey: draftKey}},
		infi.WithIfMatch(saved.DraftHash))
	if err != nil {
		return err
	}
	productID := published.Product.Id
	if productID == "" {
		return errors.New("the product was not published with an id")
	}
	setup.Show("productId", productID)

	setup.Step("Set the delivery: the buyer receives this URL after paying")
	if _, err := client.Products.SetDeliverable(ctx, infi.ProductsSetDeliverableInput{
		Path: infi.ProductsSetDeliverablePath{ProductId: productID},
		Body: infi.PutDeliverableRequest{Kind: infi.PutDeliverableRequestKindLink, Url: infi.Ptr("https://example.com/guide.pdf")},
	}); err != nil {
		return err
	}
	deliverable, err := client.Products.GetDeliverable(ctx, infi.ProductsGetDeliverableInput{Path: infi.ProductsGetDeliverablePath{ProductId: productID}})
	if err != nil {
		return err
	}
	setup.Show("delivery", deliverable)

	setup.Step("Subscribe a webhook to learn about the payment")
	// In the sandbox it applies immediately and returns the secret once; in live mode it returns approvalUrl for a person to approve.
	// If the sandbox doesn't have the secret vault configured yet, it answers 503: the step is skipped.
	var apiErr *infi.Error
	skipped := func(err error) bool { return errors.As(err, &apiErr) && apiErr.Status == 503 }
	hook, err := client.Webhooks.Subscribe(ctx, infi.WebhooksSubscribeInput{
		Body: infi.WebhookSubscribeRequest{Url: "https://example.com/hooks/infi", Events: []string{"payment.confirmed"}},
	})
	var list *infi.WebhookEndpointList
	if err == nil {
		desc := string(hook.Status)
		if hook.Secret != nil && *hook.Secret != "" {
			desc += " (secret received)"
		}
		if hook.ApprovalUrl != nil && *hook.ApprovalUrl != "" {
			desc += " approve at " + *hook.ApprovalUrl
		}
		setup.Show("webhook", desc)
		list, err = client.Webhooks.List(ctx)
	}
	switch {
	case skipped(err):
		setup.Show("webhook", "503: the sandbox doesn't store webhook secrets yet")
	case err != nil:
		return err
	default:
		urls := make([]string, 0, len(list.Data))
		for _, w := range list.Data {
			urls = append(urls, w.Url)
		}
		setup.Show("webhooks", urls)
	}

	setup.Step("Open a buyer's checkout")
	checkout, err := client.Checkouts.Create(ctx, infi.CheckoutsCreateInput{Body: infi.CheckoutsCreateJSONRequestBody{
		ProductId: productID,
		Customer: infi.CheckoutCreateRequest_Customer{
			ExternalId: "buyer_" + setup.Uniq(),
			Email:      infi.Ptr("buyer@example.com"),
		},
	}})
	if err != nil {
		return err
	}
	setup.Show("checkout", fmt.Sprintf("%s %s %s %s", checkout.Id, checkout.Status, checkout.Total, checkout.Currency))
	setup.Show("send the buyer to", checkout.Url)
	if !strings.Contains(checkout.Url, "/invoices/"+checkout.Id) {
		return errors.New("checkout url does not point to the invoice")
	}

	setup.Step("Read the checkout: this is how the code waits for the payment")
	again, err := client.Checkouts.Get(ctx, infi.CheckoutsGetInput{Path: infi.CheckoutsGetPath{CheckoutId: checkout.Id}})
	if err != nil {
		return err
	}
	setup.Show("status", again.Status)
	setup.Show("payments", len(again.Payments))
	if again.Id != checkout.Id {
		return errors.New("the checkout read is not the one created")
	}
	return nil
}
