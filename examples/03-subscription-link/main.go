// Monthly subscription: same flow of draft -> preview -> publish.
package main

import (
	"context"
	"fmt"

	infi "github.com/Be-Infinitum/infi-go"
	"github.com/Be-Infinitum/infi-go/examples/internal/setup"
)

func main() { setup.Run(run) }

func run(ctx context.Context, client *infi.Client) error {
	offer := func(description string) infi.ProductOfferValues {
		return infi.ProductOfferValues{
			Name:         "Monthly Club",
			Description:  infi.Ptr(description),
			PricingModel: infi.ProductOfferValuesPricingModelSubscription,
			Currency:     "BRL",
			BasePrice:    infi.Ptr("29.90"),
			BillingCycle: infi.Ptr(infi.ProductOfferValuesBillingCycleMonthly),
		}
	}

	setup.Step("Save the monthly subscription draft")
	productKey := setup.UUID()
	saved, err := client.Products.SaveDraft(ctx, infi.ProductsSaveDraftInput{
		Path: infi.ProductsSaveDraftPath{DraftKey: productKey},
		Body: offer("New content every month"),
	})
	if err != nil {
		return err
	}
	setup.Show("draftHash", saved.DraftHash)

	setup.Step("Read the product preview with ?hash=")
	preview, err := client.Products.GetDraft(ctx, infi.ProductsGetDraftInput{
		Path:  infi.ProductsGetDraftPath{DraftKey: productKey},
		Query: infi.ProductsGetDraftQuery{Hash: infi.Ptr(saved.DraftHash)},
	})
	if err != nil {
		return err
	}
	cycle := ""
	if preview.Offer.BillingCycle != nil {
		cycle = string(*preview.Offer.BillingCycle)
	}
	setup.Show("offer", fmt.Sprintf("%s — %s %s/%s", preview.Offer.Name, setup.Deref(preview.Offer.BasePrice, ""), preview.Offer.Currency, cycle))

	setup.Step("Save the link draft")
	linkKey := setup.UUID()
	link, err := client.Links.SaveDraft(ctx, infi.LinksSaveDraftInput{
		Path: infi.LinksSaveDraftPath{DraftKey: linkKey},
		Body: infi.SaveLinkDraftRequest{
			Product:    infi.SaveLinkDraftRequest_Product{DraftKey: infi.Ptr(productKey)},
			SuccessUrl: infi.Ptr("https://example.com/welcome"),
			CancelUrl:  infi.Ptr("https://example.com/back"),
		},
	})
	if err != nil {
		return err
	}
	setup.Show("link preview", link.Preview)

	setup.Step("Editing the product after the preview invalidates the link hash (412)")
	edited, err := client.Products.SaveDraft(ctx, infi.ProductsSaveDraftInput{
		Path: infi.ProductsSaveDraftPath{DraftKey: productKey},
		Body: offer("New content every month + community"),
	}, infi.WithIfMatch(saved.DraftHash))
	if err != nil {
		return err
	}
	setup.Show("new product draftHash", edited.DraftHash)
	_, err = client.Links.PublishDraft(ctx,
		infi.LinksPublishDraftInput{Path: infi.LinksPublishDraftPath{DraftKey: linkKey}},
		infi.WithIfMatch(link.DraftHash))
	stale, err := setup.ExpectStatus(412, err)
	if err != nil {
		return err
	}
	setup.Show("infi.Error", fmt.Sprintf("status=%d code=%s", stale.Status, stale.Code))

	setup.Step("Re-read the link preview (new hash) and publish")
	fresh, err := client.Links.GetDraft(ctx, infi.LinksGetDraftInput{Path: infi.LinksGetDraftPath{DraftKey: linkKey}})
	if err != nil {
		return err
	}
	published, err := client.Links.PublishDraft(ctx,
		infi.LinksPublishDraftInput{Path: infi.LinksPublishDraftPath{DraftKey: linkKey}},
		infi.WithIfMatch(fresh.DraftHash))
	if err != nil {
		return err
	}
	url := ""
	if published.Link != nil {
		url = setup.Deref(published.Link.Url, "")
	}
	if url == "" {
		setup.Show("Link URL", "(no url)")
	} else {
		setup.Show("Link URL", url)
	}
	if !published.Published || url == "" {
		return fmt.Errorf("link was not published with a url")
	}
	return nil
}
