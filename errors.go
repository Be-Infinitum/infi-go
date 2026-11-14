package infi

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Error is the error returned by the API (or a network error, with Status 0).
type Error struct {
	Status   int
	Code     string
	Message  string
	TracerID string
	// Details carries the per-field errors (422) or the metadata (409), when the server sends them.
	Details any
	// Body is the decoded error response body (or the raw text, if it is not JSON).
	Body any
}

func (e *Error) Error() string {
	return fmt.Sprintf("infi: %s (status %d, code %s)", e.Message, e.Status, e.Code)
}

// Current returns, on 412, the current state sent by the server (to show
// the preview again). In other cases, nil.
func (e *Error) Current() any {
	if e.Status != 412 {
		return nil
	}
	if m, ok := e.Body.(map[string]any); ok {
		return m["current"]
	}
	return nil
}

func str(m map[string]any, key string) (string, bool) {
	s, ok := m[key].(string)
	return s, ok
}

// parseError decodes an error envelope (flat or nested under "error").
func parseError(status int, text string) *Error {
	var body any
	if text != "" {
		if err := json.Unmarshal([]byte(text), &body); err != nil {
			body = nil
		}
	}
	obj, _ := body.(map[string]any)
	nested, _ := obj["error"].(map[string]any)

	code := "http_" + strconv.Itoa(status)
	if s, ok := str(obj, "error_code"); ok {
		code = s
	} else if s, ok := str(nested, "code"); ok {
		code = s
	}
	message := text
	if message == "" {
		message = "HTTP " + strconv.Itoa(status)
	}
	if s, ok := str(obj, "message"); ok {
		message = s
	} else if s, ok := str(nested, "message"); ok {
		message = s
	}
	tracer := ""
	if s, ok := str(obj, "tracer_id"); ok {
		tracer = s
	} else if s, ok := str(nested, "request_id"); ok {
		tracer = s
	}
	var details any
	if v := obj["errors"]; v != nil {
		details = v
	} else if v := obj["metadata"]; v != nil {
		details = v
	}
	var raw any = body
	if raw == nil && text != "" {
		raw = text
	}
	return &Error{Status: status, Code: code, Message: message, TracerID: tracer, Details: details, Body: raw}
}
