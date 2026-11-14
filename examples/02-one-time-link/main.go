// One-time product: product draft -> preview -> link draft -> publish with If-Match.
package main

import (
	"context"
	"fmt"

	infi "github.com/Be-Infinitum/infi-go"
	"github.com/Be-Infinitum/infi-go/examples/internal/setup"
)

func main() { setup.Run(run) }

func run(ctx context.Context, client *infi.Client) error {
	setup.Step("Save the one-time product draft")
	productKey := setup.UUID()
	saved, err := client.Products.SaveDraft(ctx, infi.ProductsSaveDraftInput{
		Path: infi.ProductsSaveDraftPath{DraftKey: productKey},
		Body: infi.ProductOfferValues{
			Name:         "1h Mentoring",
			Description:  infi.Ptr("A one-on-one session"),
			PricingModel: infi.ProductOfferValuesPricingModelOneTime,
			Currency:     "BRL",
			BasePrice:    infi.Ptr("150.00"),
		},
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
	setup.Show("offer", fmt.Sprintf("%s — %s %s", preview.Offer.Name, setup.Deref(preview.Offer.BasePrice, ""), preview.Offer.Currency))
	setup.Show("published", preview.Published)

	setup.Step("Save the link draft pointing to the product draft")
	linkKey := setup.UUID()
	link, err := client.Links.SaveDraft(ctx, infi.LinksSaveDraftInput{
		Path: infi.LinksSaveDraftPath{DraftKey: linkKey},
		Body: infi.SaveLinkDraftRequest{
			Product:    infi.SaveLinkDraftRequest_Product{DraftKey: infi.Ptr(productKey)},
			SuccessUrl: infi.Ptr("https://example.com/thank-you"),
		},
	})
	if err != nil {
		return err
	}
	setup.Show("link preview", link.Preview)
	setup.Show("link draftHash", link.DraftHash)

	setup.Step("Publishing the link without If-Match should give 428")
	_, err = client.Links.PublishDraft(ctx, infi.LinksPublishDraftInput{Path: infi.LinksPublishDraftPath{DraftKey: linkKey}})
	e428, err := setup.ExpectStatus(428, err)
	if err != nil {
		return err
	}
	setup.Show("infi.Error", fmt.Sprintf("status=%d code=%s", e428.Status, e428.Code))

	setup.Step("Publish the link with If-Match = draftHash")
	published, err := client.Links.PublishDraft(ctx,
		infi.LinksPublishDraftInput{Path: infi.LinksPublishDraftPath{DraftKey: linkKey}},
		infi.WithIfMatch(link.DraftHash))
	if err != nil {
		return err
	}
	setup.Show("published", published.Published)
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
