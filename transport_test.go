package infi

// Transport-only tests (like http.test.ts): URL, headers, retries, errors.
// The proof that the SDK talks to the API is the examples against the sandbox
// (ADR 0055, D7); here the server is an httptest.Server, never a fake of the API.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sync"
	"testing"
	"time"
)

type call struct {
	method string
	url    string
	header http.Header
	body   string
}

type reply struct {
	status  int
	body    string
	headers map[string]string
	drop    bool // drops the connection (network error)
}

type rig struct {
	t      *testing.T
	mu     sync.Mutex
	calls  []call
	sleeps []time.Duration
	client *Client
}

// newRig starts a server that replies in sequence (the last one repeats).
func newRig(t *testing.T, key string, replies ...reply) *rig {
	t.Helper()
	r := &rig{t: t}
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.calls = append(r.calls, call{req.Method, req.URL.RequestURI(), req.Header.Clone(), string(b)})
		rp := replies[min(i, len(replies)-1)]
		i++
		r.mu.Unlock()
		if rp.drop {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		for k, v := range rp.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(rp.status)
		io.WriteString(w, rp.body)
	}))
	t.Cleanup(srv.Close)
	c, err := New(key, WithAPIURL(srv.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(_ context.Context, d time.Duration) error {
		r.sleeps = append(r.sleeps, d)
		return nil
	}
	r.client = c
	return r
}

func ok(body string) reply { return reply{status: 200, body: body} }

var recordBody = UsageRecordJSONRequestBody{CustomerId: "c", EventId: "e", Meter: "m", Value: Ptr("1")}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestPostSendsBearerAndIdempotencyKeyUUID(t *testing.T) {
	r := newRig(t, "sk_live_x", reply{status: 202, body: `{}`})
	if _, err := r.client.Usage.Record(context.Background(), UsageRecordInput{Body: recordBody}); err != nil {
		t.Fatal(err)
	}
	c := r.calls[0]
	if c.url != "/v1/usage/events" || c.method != "POST" {
		t.Fatalf("%s %s", c.method, c.url)
	}
	if c.header.Get("Authorization") != "Bearer sk_live_x" || c.header.Get("Accept") != "application/json" {
		t.Fatalf("headers: %v", c.header)
	}
	if !uuidRe.MatchString(c.header.Get("Idempotency-Key")) {
		t.Fatalf("Idempotency-Key = %q", c.header.Get("Idempotency-Key"))
	}
	if c.header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", c.header.Get("Content-Type"))
	}
}

func TestGETDoesNotSendIdempotencyKey(t *testing.T) {
	r := newRig(t, "sk_live_x", ok(`{"data":[],"nextCursor":null}`))
	if _, err := r.client.Payments.List(context.Background(), PaymentsListInput{}); err != nil {
		t.Fatal(err)
	}
	if _, has := r.calls[0].header["Idempotency-Key"]; has {
		t.Fatal("GET mandou Idempotency-Key")
	}
	if r.calls[0].url != "/v1/payments" {
		t.Fatalf("url = %s", r.calls[0].url)
	}
}

func TestBaseURLFromKeyAndWithAPIURL(t *testing.T) {
	for key, want := range map[string]string{"sk_test_x": SandboxURL, "sk_live_x": LiveURL} {
		got, err := BaseURLFor(key, "")
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", key, got, err)
		}
	}
	if got, _ := BaseURLFor("sk_live_x", "http://localhost:8088//"); got != "http://localhost:8088" {
		t.Fatalf("WithAPIURL wins and loses the trailing slashes: %q", got)
	}
	if _, err := New("bad"); err == nil {
		t.Fatal("key without prefix should fail")
	}
	if _, err := New(""); err == nil {
		t.Fatal("empty key should fail")
	}
	if _, err := New("bad", WithAPIURL("http://x")); err != nil {
		t.Fatalf("WithAPIURL waives the prefix: %v", err)
	}
}

func TestPathQueryHeadersBodyAndOptions(t *testing.T) {
	r := newRig(t, "sk_test_x", ok(`{"ok":true}`))
	ctx := context.Background()
	_, err := r.client.Customers.GetWallet(ctx, CustomersGetWalletInput{
		Path:  CustomersGetWalletPath{EnrollmentId: "c 1"},
		Query: CustomersGetWalletQuery{Meter: Ptr("m1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.client.Config.Apply(ctx, ConfigApplyInput{Body: ConfigApplyJSONRequestBody{Document: CompanyDocument{SchemaVersion: 1, Products: []CompanyProduct{}}}},
		WithIfMatch(`"h"`), WithChannel(ChannelCLI), WithIdempotencyKey("k1"))
	if err != nil {
		t.Fatal(err)
	}
	if r.calls[0].url != "/v1/customers/c%201/wallet?meter=m1" {
		t.Fatalf("url = %s", r.calls[0].url)
	}
	h := r.calls[1].header
	if h.Get("If-Match") != `"h"` || h.Get("X-Infi-Channel") != "cli" || h.Get("Idempotency-Key") != "k1" {
		t.Fatalf("headers: %v", h)
	}
	if r.calls[1].body != `{"document":{"products":[],"schemaVersion":1}}` || r.calls[1].url != "/v1/config/apply" {
		t.Fatalf("apply: %s %q", r.calls[1].url, r.calls[1].body)
	}
}

func TestParamHeaderAndPathWithoutUndueEscaping(t *testing.T) {
	r := newRig(t, "sk_test_x", ok(`{}`))
	_, err := r.client.CustomerSessions.Verify(context.Background(), CustomerSessionsVerifyInput{
		Headers: CustomerSessionsVerifyHeaders{XBuyerToken: "bt_1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.calls[0].header.Get("X-Buyer-Token") != "bt_1" {
		t.Fatalf("headers: %v", r.calls[0].header)
	}
	if got := encodeURIComponent("a/b c?é!'()*~"); got != "a%2Fb%20c%3F%C3%A9!'()*~" {
		t.Fatalf("encodeURIComponent = %q", got)
	}
}

func TestEmptyIfMatchIsStillSent(t *testing.T) {
	r := newRig(t, "sk_test_x", ok(`{}`))
	_, _ = r.client.Links.PublishDraft(context.Background(),
		LinksPublishDraftInput{Path: LinksPublishDraftPath{DraftKey: "d"}}, WithIfMatch(""))
	if _, has := r.calls[0].header["If-Match"]; !has {
		t.Fatal("explicit If-Match (even empty) must be sent")
	}
}

func TestServerErrorRetriesWithSameKeyAndSucceeds(t *testing.T) {
	r := newRig(t, "sk_live_x", reply{status: 503, body: `{}`}, reply{status: 503, body: `{}`}, reply{status: 202, body: `{"accepted":1}`})
	if _, err := r.client.Usage.Record(context.Background(), UsageRecordInput{Body: recordBody}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("calls = %d", len(r.calls))
	}
	keys := map[string]bool{}
	for _, c := range r.calls {
		keys[c.header.Get("Idempotency-Key")] = true
	}
	if len(keys) != 1 {
		t.Fatalf("the key changed between attempts: %v", keys)
	}
	if !reflect.DeepEqual(r.sleeps, []time.Duration{250 * time.Millisecond, time.Second}) {
		t.Fatalf("sleeps = %v", r.sleeps)
	}
}

func TestNetworkErrorRetriesAndGivesUpAfterThreeAttempts(t *testing.T) {
	r := newRig(t, "sk_live_x", reply{drop: true})
	_, err := r.client.Usage.Record(context.Background(), UsageRecordInput{Body: recordBody})
	var e *Error
	if !errors.As(err, &e) || e.Code != "network_error" || e.Status != 0 {
		t.Fatalf("err = %#v", err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("calls = %d", len(r.calls))
	}
	if !reflect.DeepEqual(r.sleeps, []time.Duration{250 * time.Millisecond, time.Second}) {
		t.Fatalf("sleeps = %v", r.sleeps)
	}
}

func TestFourHundredIsNotRetried(t *testing.T) {
	r := newRig(t, "sk_live_x", reply{status: 400, body: `{"error_code":"bad_request","message":"x","tracer_id":"t1"}`})
	_, err := r.client.Usage.Record(context.Background(), UsageRecordInput{Body: recordBody})
	var e *Error
	if !errors.As(err, &e) || e.Status != 400 || e.Code != "bad_request" || e.TracerID != "t1" || e.Message != "x" {
		t.Fatalf("err = %#v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d", len(r.calls))
	}
}

func TestNestedEnvelopeBecomesError(t *testing.T) {
	r := newRig(t, "sk_live_x", reply{status: 409, body: `{"error":{"code":"idempotency_key_reused","message":"m","request_id":"r9"}}`})
	_, err := r.client.Usage.Record(context.Background(), UsageRecordInput{Body: recordBody})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %#v", err)
	}
	if e.Status != 409 || e.Code != "idempotency_key_reused" || e.Message != "m" || e.TracerID != "r9" {
		t.Fatalf("e = %#v", e)
	}
}

func TestFlatEnvelopeWithErrorsKeepsDetails(t *testing.T) {
	r := newRig(t, "sk_live_x", reply{status: 422, body: `{"error_code":"validation_failed","message":"m","tracer_id":"t","errors":[{"field":"a"}]}`})
	_, err := r.client.Usage.Record(context.Background(), UsageRecordInput{Body: recordBody})
	var e *Error
	errors.As(err, &e)
	want := []any{map[string]any{"field": "a"}}
	if !reflect.DeepEqual(e.Details, want) {
		t.Fatalf("details = %#v", e.Details)
	}
}

func TestPrecondition412CarriesCurrent(t *testing.T) {
	r := newRig(t, "sk_live_x", reply{status: 412, body: `{"error_code":"plan_changed","message":"m","tracer_id":"t","current":{"planHash":"H2"}}`})
	_, err := r.client.Config.Apply(context.Background(), ConfigApplyInput{}, WithIfMatch(`"H1"`))
	var e *Error
	if !errors.As(err, &e) || e.Status != 412 {
		t.Fatalf("err = %#v", err)
	}
	want := map[string]any{"planHash": "H2"}
	if !reflect.DeepEqual(e.Current(), want) {
		t.Fatalf("current = %#v", e.Current())
	}
	if (&Error{Status: 400, Body: map[string]any{"current": 1}}).Current() != nil {
		t.Fatal("Current only applies to 412")
	}
}

func TestRetryAfterIsHonoredOn429(t *testing.T) {
	r := newRig(t, "sk_live_x",
		reply{status: 429, body: `{"error":{"code":"rate_limited","message":"m","request_id":"r"}}`, headers: map[string]string{"Retry-After": "2"}},
		reply{status: 202, body: `{}`})
	if _, err := r.client.Usage.Record(context.Background(), UsageRecordInput{Body: recordBody}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.sleeps, []time.Duration{2 * time.Second}) {
		t.Fatalf("sleeps = %v", r.sleeps)
	}
}

func TestRetryAfterAsDate(t *testing.T) {
	d, ok := retryAfter(time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat))
	if !ok || d < 8*time.Second || d > 10*time.Second {
		t.Fatalf("d = %v", d)
	}
	if _, ok := retryAfter("tomorrow"); ok {
		t.Fatal("garbage is not Retry-After")
	}
	if d, ok := retryAfter("-3"); !ok || d != 0 {
		t.Fatalf("negative becomes 0: %v %v", d, ok)
	}
}

func TestNoContentReturnsZeroValue(t *testing.T) {
	r := newRig(t, "sk_test_x", reply{status: 204})
	out, err := r.client.Checkouts.Get(context.Background(), CheckoutsGetInput{Path: CheckoutsGetPath{CheckoutId: "x"}})
	if err != nil || out == nil || out.Id != "" {
		t.Fatalf("out = %#v err = %v", out, err)
	}
}

func TestCancelledContext(t *testing.T) {
	r := newRig(t, "sk_live_x", reply{status: 503, body: `{}`})
	ctx, cancel := context.WithCancel(context.Background())
	r.client.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, err := r.client.Usage.Record(ctx, UsageRecordInput{Body: recordBody})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d", len(r.calls))
	}
	// and before starting: no call goes out
	if _, err := r.client.Payments.List(ctx, PaymentsListInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestWithHTTPClientAndMaxAttempts(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(500)
	}))
	defer srv.Close()
	c, err := New("sk_test_x", WithAPIURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxAttempts(2))
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	_, err = c.Payments.List(context.Background(), PaymentsListInput{})
	var e *Error
	if !errors.As(err, &e) || e.Status != 500 || e.Code != "http_500" || hits != 2 {
		t.Fatalf("err = %#v hits = %d", err, hits)
	}
}

func TestParseError(t *testing.T) {
	e := parseError(502, "bad gateway")
	if e.Code != "http_502" || e.Message != "bad gateway" || e.Body != "bad gateway" {
		t.Fatalf("%#v", e)
	}
	e = parseError(500, "")
	if e.Message != "HTTP 500" || e.Body != nil {
		t.Fatalf("%#v", e)
	}
	e = parseError(409, `{"error_code":"x","message":"m","metadata":{"a":1}}`)
	if !reflect.DeepEqual(e.Details, map[string]any{"a": float64(1)}) {
		t.Fatalf("%#v", e.Details)
	}
	if e.Error() == "" {
		t.Fatal("Error() empty")
	}
}

func TestPaginationFollowsNextCursorToTheEnd(t *testing.T) {
	pages := map[string]struct {
		data []int
		next *string
	}{
		"":  {[]int{1, 2}, Ptr("a")},
		"a": {[]int{3}, Ptr("b")},
		"b": {[]int{4}, nil},
	}
	var seen []string
	var out []int
	for n, err := range Paginate(func(cursor *string) ([]int, *string, error) {
		k := ""
		if cursor != nil {
			k = *cursor
		}
		seen = append(seen, k)
		p := pages[k]
		return p.data, p.next, nil
	}) {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if !reflect.DeepEqual(out, []int{1, 2, 3, 4}) || !reflect.DeepEqual(seen, []string{"", "a", "b"}) {
		t.Fatalf("out = %v seen = %v", out, seen)
	}
}

func TestPaginationStopsOnErrorAndBreak(t *testing.T) {
	boom := errors.New("boom")
	n := 0
	for _, err := range Paginate(func(cursor *string) ([]int, *string, error) {
		n++
		if cursor != nil {
			return nil, nil, boom
		}
		return []int{1}, Ptr("x"), nil
	}) {
		if err != nil {
			if !errors.Is(err, boom) {
				t.Fatal(err)
			}
			break
		}
	}
	if n != 2 {
		t.Fatalf("fetch called %d times", n)
	}
	calls := 0
	for range PaginateFrom(Ptr("start"), func(cursor *string) ([]int, *string, error) {
		calls++
		if *cursor != "start" {
			t.Fatalf("cursor = %q", *cursor)
		}
		return []int{1, 2}, Ptr("more"), nil
	}) {
		break
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestPaginationWithRealList(t *testing.T) {
	r := newRig(t, "sk_test_x",
		ok(`{"data":[{"id":"1"},{"id":"2"}],"nextCursor":"c2"}`),
		ok(`{"data":[{"id":"3"}],"nextCursor":null}`))
	var ids []string
	for p, err := range Paginate(func(cursor *string) ([]Payment, *string, error) {
		l, err := r.client.Payments.List(context.Background(), PaymentsListInput{Query: PaymentsListQuery{Limit: Ptr(2), Cursor: cursor}})
		if err != nil {
			return nil, nil, err
		}
		return l.Data, l.NextCursor, nil
	}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.Id)
	}
	if !reflect.DeepEqual(ids, []string{"1", "2", "3"}) {
		t.Fatalf("ids = %v", ids)
	}
	if r.calls[0].url != "/v1/payments?limit=2" || r.calls[1].url != "/v1/payments?cursor=c2&limit=2" {
		t.Fatalf("urls = %s | %s", r.calls[0].url, r.calls[1].url)
	}
}

func TestEveryContractOperationHasAClientMethod(t *testing.T) {
	if len(Operations) != 28 {
		t.Fatalf("operations = %d, expected 28", len(Operations))
	}
	c, err := New("sk_test_x")
	if err != nil {
		t.Fatal(err)
	}
	rv := reflect.ValueOf(c).Elem()
	for _, o := range Operations {
		svc := rv.FieldByName(o.Service)
		if !svc.IsValid() || svc.IsNil() {
			t.Errorf("%s: service %s missing", o.OperationID, o.Service)
			continue
		}
		m := svc.MethodByName(o.Method)
		if !m.IsValid() {
			t.Errorf("%s: method %s.%s missing", o.OperationID, o.Service, o.Method)
			continue
		}
		if m.Type().In(0).String() != "context.Context" {
			t.Errorf("%s: the first argument should be context.Context", o.OperationID)
		}
	}
}

func TestExpectedNames(t *testing.T) {
	c, _ := New("sk_test_x")
	_ = c.Checkouts.Create
	_ = c.CustomerSessions.Create
	_ = c.Products.SetDeliverable
	_ = c.Webhooks.Subscribe
	_ = c.Customers.GetWallet
	_ = c.Config.Plan
}

func TestUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		u := newUUID()
		if !uuidRe.MatchString(u) || seen[u] {
			t.Fatalf("invalid or repeated uuid: %s", u)
		}
		seen[u] = true
	}
}
