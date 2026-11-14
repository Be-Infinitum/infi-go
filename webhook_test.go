package infi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"
	"time"
)

const (
	whSecret = "whsec_test"
	whBody   = `{"type":"payment.succeeded"}`
)

func sign(id string, ts int64, body, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(id + "." + strconv.FormatInt(ts, 10) + "." + body))
	return "v1=" + hex.EncodeToString(m.Sum(nil))
}

func whReq(now int64, mut func(*WebhookRequest)) WebhookRequest {
	r := WebhookRequest{ID: "evt_1", Timestamp: strconv.FormatInt(now, 10), Signature: sign("evt_1", now, whBody, whSecret), Body: []byte(whBody)}
	if mut != nil {
		mut(&r)
	}
	return r
}

func nowSec() int64 { return time.Now().Unix() }

func TestFixedVectorFromTypeScriptSDK(t *testing.T) {
	// Signature computed with Node's crypto.createHmac (webhooks.ts):
	// id=evt_1, timestamp=1700000000, secret=whsec_test.
	const want = "v1=ebd25a3c85bf535ae3277da3d4886fba12ec8b8753e8087b498cc4b5f9512167"
	if got := sign("evt_1", 1700000000, whBody, whSecret); got != want {
		t.Fatalf("reference signature diverged: %s", got)
	}
	opts := WebhookOptions{Now: func() time.Time { return time.Unix(1700000100, 0) }}
	r := WebhookRequest{ID: "evt_1", Timestamp: "1700000000", Signature: want, Body: []byte(whBody)}
	if !VerifyWebhookWithOptions(r, opts, whSecret) {
		t.Fatal("the fixed vector should pass")
	}
}

func TestValidSignaturePasses(t *testing.T) {
	if !VerifyWebhook(whReq(nowSec(), nil), whSecret) {
		t.Fatal("should pass")
	}
}

func TestMultipleSignaturesAndSecretRotation(t *testing.T) {
	n := nowSec()
	sig := sign("evt_1", n, whBody, "old") + " " + sign("evt_1", n, whBody, whSecret)
	if !VerifyWebhook(whReq(n, func(r *WebhookRequest) { r.Signature = sig }), whSecret) {
		t.Fatal("multiple signatures")
	}
	if !VerifyWebhook(whReq(n, nil), "outro", whSecret) {
		t.Fatal("multiple secrets")
	}
}

func TestTamperedBodyFails(t *testing.T) {
	if VerifyWebhook(whReq(nowSec(), func(r *WebhookRequest) { r.Body = []byte(whBody + " ") }), whSecret) {
		t.Fatal("tampered body passed")
	}
}

func TestWrongIDSecretOrFormatFail(t *testing.T) {
	n := nowSec()
	if VerifyWebhook(whReq(n, func(r *WebhookRequest) { r.ID = "evt_2" }), whSecret) {
		t.Fatal("wrong id")
	}
	if VerifyWebhook(whReq(n, nil), "outro") {
		t.Fatal("wrong secret")
	}
	if VerifyWebhook(whReq(n, func(r *WebhookRequest) { r.Signature = "abc" }), whSecret) {
		t.Fatal("wrong format")
	}
	if VerifyWebhook(whReq(n, func(r *WebhookRequest) { r.Signature = "v2=" + r.Signature[3:] }), whSecret) {
		t.Fatal("unknown version")
	}
	if VerifyWebhook(whReq(n, func(r *WebhookRequest) { r.Signature = "v1=" }), whSecret) {
		t.Fatal("empty signature")
	}
	if VerifyWebhook(whReq(n, nil)) {
		t.Fatal("never passes without a secret")
	}
}

func TestOldOrFutureTimestampFails(t *testing.T) {
	n := nowSec()
	old := n - 400
	r := whReq(old, nil)
	if VerifyWebhook(r, whSecret) {
		t.Fatal("old timestamp passed")
	}
	if !VerifyWebhookWithOptions(r, WebhookOptions{Tolerance: 500 * time.Second}, whSecret) {
		t.Fatal("500s tolerance should accept")
	}
	if VerifyWebhook(whReq(n+400, nil), whSecret) {
		t.Fatal("future timestamp passed")
	}
}

func TestNonIntegerTimestamp(t *testing.T) {
	n := nowSec()
	for _, ts := range []string{"abc", "1.5", "", "Infinity", "NaN"} {
		if VerifyWebhook(whReq(n, func(r *WebhookRequest) { r.Timestamp = ts }), whSecret) {
			t.Fatalf("timestamp %q passed", ts)
		}
	}
	// like Number("123.0"): an integer
	if !VerifyWebhook(whReq(n, func(r *WebhookRequest) { r.Timestamp = strconv.FormatInt(n, 10) + ".0" }), whSecret) {
		t.Fatal("timestamp with .0 should be valid")
	}
}

func TestHexLikeNodeBufferFrom(t *testing.T) {
	// Buffer.from("abz1","hex") -> <ab>; "abc" -> <ab>; uppercase is valid.
	if got := nodeHexDecode("abz1"); len(got) != 1 || got[0] != 0xab {
		t.Fatalf("%x", got)
	}
	if got := nodeHexDecode("abc"); len(got) != 1 {
		t.Fatalf("%x", got)
	}
	if got := nodeHexDecode("ABCD"); len(got) != 2 || got[1] != 0xcd {
		t.Fatalf("%x", got)
	}
	n := nowSec()
	if !VerifyWebhook(whReq(n, func(r *WebhookRequest) { r.Signature += "zz" }), whSecret) {
		t.Fatal("Node decodes hex up to the first invalid pair; the Go SDK does the same")
	}
}

func TestWebhookRequestFrom(t *testing.T) {
	h := http.Header{}
	h.Set("X-Webhook-Id", "evt_1")
	h.Set("X-Webhook-Timestamp", "123")
	h.Set("X-Webhook-Signature", "v1=ab")
	r := WebhookRequestFrom(h, []byte("x"))
	if r.ID != "evt_1" || r.Timestamp != "123" || r.Signature != "v1=ab" || string(r.Body) != "x" {
		t.Fatalf("%#v", r)
	}
}
