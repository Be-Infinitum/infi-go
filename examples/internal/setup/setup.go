// Package setup holds the examples' helpers: a ready client, printed steps and assertions.
package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	infi "github.com/Be-Infinitum/infi-go"
)

// Run creates the client on a sandbox account that already has an owner, runs fn and exits with
// code 1 if it fails. The anonymous account (/public/v1/claimables) was removed in ADR
// 0076 and answers 410: the key comes from an account created through agent
// sign-up or the dashboard.
func Run(fn func(ctx context.Context, client *infi.Client) error) {
	secretKey := os.Getenv("INFI_SECRET_KEY")
	if secretKey == "" {
		fmt.Fprintln(os.Stderr, "set INFI_SECRET_KEY to an sk_test_ key from your own sandbox account")
		os.Exit(1)
	}
	var opts []infi.Option
	if apiURL := strings.TrimRight(os.Getenv("INFI_API_URL"), "/"); apiURL != "" {
		opts = append(opts, infi.WithAPIURL(apiURL))
	}
	client, err := infi.New(secretKey, opts...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := fn(ctx, client); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		stop()
		os.Exit(1)
	}
}

var n int

// Step prints the title of the next step.
func Step(title string) {
	n++
	fmt.Printf("\n── %d. %s\n", n, title)
}

// Show prints a label and a value (text as is, anything else as JSON).
func Show(label string, value any) {
	text, ok := value.(string)
	if !ok {
		b, err := json.Marshal(value)
		if err != nil {
			text = fmt.Sprint(value)
		} else {
			text = string(b)
		}
	}
	fmt.Printf("   %s: %s\n", label, text)
}

// Sleep waits for d or for the context to end.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func randomHex(nbytes int) []byte {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// Uniq returns a short suffix for keys that are unique per run.
func Uniq() string { return hex.EncodeToString(randomHex(4)) }

// UUID returns a UUID v4 (draft keys are UUIDs chosen by the caller).
func UUID() string {
	b := randomHex(16)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// Len is the length of an optional list (a nil pointer counts as 0).
func Len[T any](p *[]T) int {
	if p == nil {
		return 0
	}
	return len(*p)
}

// Deref returns the pointed-to value, or fallback if the pointer is nil.
func Deref[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

// ExpectStatus expects err to be an *infi.Error with the given status; it fails if
// it is not (including when the call succeeded).
func ExpectStatus(status int, err error) (*infi.Error, error) {
	if err == nil {
		return nil, fmt.Errorf("expected HTTP %d, but the call succeeded", status)
	}
	var e *infi.Error
	if !errors.As(err, &e) {
		return nil, err
	}
	if e.Status != status {
		return nil, fmt.Errorf("expected HTTP %d, got %d (%s): %s", status, e.Status, e.Code, e.Message)
	}
	return e, nil
}
