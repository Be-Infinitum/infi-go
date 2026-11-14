package infi

// Webhook verification. Scheme from backend/internal/messaging/webhook/signer.go:
//
//	signed_content = id + "." + timestamp + "." + body
//	signature      = "v1=" + hex(HMAC_SHA256(secret, signed_content))
//
// The header may carry several signatures separated by spaces (rotation).
// Same algorithm as webhooks.ts in the TypeScript SDK.

import (
	"crypto/hmac"
	"crypto/sha256"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// WebhookRequest gathers what verification needs from a delivery.
type WebhookRequest struct {
	// ID is the X-Webhook-Id header.
	ID string
	// Timestamp is the X-Webhook-Timestamp header (Unix seconds).
	Timestamp string
	// Signature is the X-Webhook-Signature header.
	Signature string
	// Body is the raw body, exactly as received.
	Body []byte
}

// WebhookRequestFrom builds the WebhookRequest from the headers and the raw body.
func WebhookRequestFrom(h http.Header, body []byte) WebhookRequest {
	return WebhookRequest{
		ID:        h.Get("X-Webhook-Id"),
		Timestamp: h.Get("X-Webhook-Timestamp"),
		Signature: h.Get("X-Webhook-Signature"),
		Body:      body,
	}
}

// WebhookOptions adjusts verification.
type WebhookOptions struct {
	// Tolerance is the maximum difference between the timestamp and now (default 5 minutes).
	Tolerance time.Duration
	// Now is the clock (default time.Now); useful in tests.
	Now func() time.Time
}

// VerifyWebhook checks the signature against one or more secrets (rotation) with the
// default tolerance of 5 minutes.
func VerifyWebhook(req WebhookRequest, secrets ...string) bool {
	return VerifyWebhookWithOptions(req, WebhookOptions{}, secrets...)
}

// VerifyWebhookWithOptions is VerifyWebhook with configurable tolerance and clock.
func VerifyWebhookWithOptions(req WebhookRequest, opts WebhookOptions, secrets ...string) bool {
	tol := opts.Tolerance
	if tol == 0 {
		tol = 300 * time.Second
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	// JavaScript's Number(timestamp): empty is 0, and it must be an integer.
	raw := strings.TrimSpace(req.Timestamp)
	tsf := 0.0
	if raw != "" {
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return false
		}
		tsf = f
	}
	if math.IsInf(tsf, 0) || math.IsNaN(tsf) || tsf != math.Trunc(tsf) {
		return false
	}
	nowSec := float64(now().UnixMilli()) / 1000
	if math.Abs(nowSec-tsf) > tol.Seconds() {
		return false
	}
	ts := int64(tsf)

	var candidates [][]byte
	for _, part := range strings.Fields(req.Signature) {
		i := strings.IndexByte(part, '=')
		if i < 0 || part[:i] != "v1" || i == len(part)-1 {
			continue
		}
		candidates = append(candidates, nodeHexDecode(part[i+1:]))
	}
	if len(candidates) == 0 {
		return false
	}

	prefix := req.ID + "." + strconv.FormatInt(ts, 10) + "."
	for _, key := range secrets {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(prefix))
		mac.Write(req.Body)
		want := mac.Sum(nil)
		for _, got := range candidates {
			if len(got) == len(want) && hmac.Equal(got, want) {
				return true
			}
		}
	}
	return false
}

// nodeHexDecode decodes hex like Node's Buffer.from(s, "hex"): stops at the
// first invalid pair and ignores a trailing lone digit.
func nodeHexDecode(s string) []byte {
	out := make([]byte, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		hi, ok1 := hexVal(s[i])
		lo, ok2 := hexVal(s[i+1])
		if !ok1 || !ok2 {
			break
		}
		out = append(out, hi<<4|lo)
	}
	return out
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
