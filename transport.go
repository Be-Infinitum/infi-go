package infi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Wait between attempts (index = the attempt that failed).
var backoff = [...]time.Duration{250 * time.Millisecond, time.Second, 4 * time.Second}

func backoffFor(attempt int) time.Duration {
	if attempt < len(backoff) {
		return backoff[attempt]
	}
	return 4 * time.Second
}

// request is a call already translated by the generated code. pathParams, query and
// headers are structs with `path`, `query` and `header` tags.
type request struct {
	method     string
	path       string // template with {param}
	pathParams any
	query      any
	headers    any
	body       any
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryAfter reads Retry-After (seconds or HTTP date).
func retryAfter(h string) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if s, err := strconv.ParseFloat(h, 64); err == nil && s == s && s < 1e12 && s > -1e12 {
		if s < 0 {
			s = 0
		}
		return time.Duration(s * float64(time.Second)), true
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// encodeURIComponent like JavaScript's: only A-Z a-z 0-9 - _ . ! ~ * ' ( ) are kept.
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			strings.IndexByte("-_.!~*'()", c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// fields walks a struct with `tag` tags and returns (name, value) of the fields
// that are present (nil pointers are omitted), in field order.
func fields(v any, tag string) ([][2]string, error) {
	if v == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("infi: %s params must be a struct, got %s", tag, rv.Kind())
	}
	var out [][2]string
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Tag.Get(tag)
		if name == "" {
			continue
		}
		f := rv.Field(i)
		for f.Kind() == reflect.Pointer {
			if f.IsNil() {
				break
			}
			f = f.Elem()
		}
		if f.Kind() == reflect.Pointer { // nil
			continue
		}
		var s string
		switch f.Kind() {
		case reflect.String:
			s = f.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			s = strconv.FormatInt(f.Int(), 10)
		case reflect.Bool:
			s = strconv.FormatBool(f.Bool())
		case reflect.Float32, reflect.Float64:
			s = strconv.FormatFloat(f.Float(), 'f', -1, 64)
		default:
			return nil, fmt.Errorf("infi: parameter %s has unsupported type %s", name, f.Kind())
		}
		out = append(out, [2]string{name, s})
	}
	return out, nil
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hex[c>>4], hex[c&15])
	}
	return string(out)
}

func isNilBody(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// do builds the request, retries when appropriate and decodes the 2xx into out (if not nil).
func (c *Client) do(ctx context.Context, req request, opts []CallOption, out any) error {
	var cc callConfig
	for _, o := range opts {
		o(&cc)
	}

	pvals, err := fields(req.pathParams, "path")
	if err != nil {
		return err
	}
	path := req.path
	for _, kv := range pvals {
		path = strings.ReplaceAll(path, "{"+kv[0]+"}", encodeURIComponent(kv[1]))
	}
	u := c.baseURL + path
	qvals, err := fields(req.query, "query")
	if err != nil {
		return err
	}
	if len(qvals) > 0 {
		q := url.Values{}
		for _, kv := range qvals {
			q.Set(kv[0], kv[1])
		}
		u += "?" + q.Encode()
	}

	method := strings.ToUpper(req.method)
	h := http.Header{}
	h.Set("Accept", "application/json")
	h.Set("Authorization", "Bearer "+c.secretKey)
	h.Set("User-Agent", UserAgent)
	hvals, err := fields(req.headers, "header")
	if err != nil {
		return err
	}
	for _, kv := range hvals {
		h.Set(kv[0], kv[1])
	}
	if method != http.MethodGet && method != http.MethodHead {
		key := cc.idempotencyKey
		if key == "" {
			key = newUUID() // a single key for all attempts
		}
		h.Set("Idempotency-Key", key)
	}
	if cc.ifMatch != nil {
		h.Set("If-Match", *cc.ifMatch)
	}
	if cc.channel != "" {
		h.Set("X-Infi-Channel", string(cc.channel))
	}
	var payload []byte
	if !isNilBody(req.body) {
		h.Set("Content-Type", "application/json")
		if payload, err = json.Marshal(req.body); err != nil {
			return fmt.Errorf("infi: request body: %w", err)
		}
	}

	var lastErr *Error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		last := attempt == c.maxAttempts-1
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		hreq, err := http.NewRequestWithContext(ctx, method, u, body)
		if err != nil {
			return fmt.Errorf("infi: %w", err)
		}
		hreq.Header = h.Clone()

		res, err := c.http.Do(hreq)
		var text []byte
		if err == nil {
			text, err = io.ReadAll(res.Body)
			res.Body.Close()
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			msg := err.Error()
			if msg == "" {
				msg = "network error"
			}
			lastErr = &Error{Status: 0, Code: "network_error", Message: msg}
			if last {
				return lastErr
			}
			if err := c.sleep(ctx, backoffFor(attempt)); err != nil {
				return err
			}
			continue
		}

		if res.StatusCode >= 200 && res.StatusCode < 300 {
			if res.StatusCode == http.StatusNoContent || len(text) == 0 || out == nil {
				return nil
			}
			if err := json.Unmarshal(text, out); err != nil {
				return fmt.Errorf("infi: invalid response (status %d): %w", res.StatusCode, err)
			}
			return nil
		}
		lastErr = parseError(res.StatusCode, string(text))
		retryable := res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500
		if !retryable || last {
			return lastErr
		}
		wait, ok := retryAfter(res.Header.Get("Retry-After"))
		if !ok {
			wait = backoffFor(attempt)
		}
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
	}
	if lastErr == nil {
		return errors.New("infi: no attempt was made")
	}
	return lastErr
}
