package main

import (
	"strings"
	"testing"
)

func op(over obj) obj {
	o := obj{
		"operationId":   "payments_list",
		"tags":          []any{"payments"},
		"x-infi-facade": true,
		"x-infi-scope":  "payments:read",
		"responses":     obj{"200": obj{"content": obj{"application/json": obj{"schema": obj{"$ref": "#/components/schemas/PaymentList"}}}}},
	}
	for k, v := range over {
		if v == nil {
			delete(o, k)
		} else {
			o[k] = v
		}
	}
	return o
}

func doc(o obj) obj { return obj{"paths": obj{"/v1/payments": obj{"get": o}}} }

func wantErr(t *testing.T, d obj, sub string) {
	t.Helper()
	_, err := collectOps(d)
	if err == nil || !strings.Contains(err.Error(), sub) {
		t.Fatalf("expected error containing %q, got %v", sub, err)
	}
}

func TestAcceptsValidOperationAndBuildsPascalName(t *testing.T) {
	d := doc(op(obj{"operationId": "customers_get_wallet", "tags": []any{"customers"}}))
	ops, err := collectOps(d)
	if err != nil {
		t.Fatal(err)
	}
	if ops[0].Action != "get_wallet" {
		t.Fatalf("action = %q", ops[0].Action)
	}
	d["components"] = obj{}
	src, err := generate(d, ops)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "GetWallet(") {
		t.Fatal("the generated code has no GetWallet(")
	}
	if !strings.HasPrefix(string(src), "// Code generated ") || !strings.Contains(string(src), "DO NOT EDIT.") {
		t.Fatal("missing generated-code header")
	}
}

func TestRejectsOperationIDOutsidePattern(t *testing.T) {
	wantErr(t, doc(op(obj{"operationId": "listPayments"})), `payments_<snake_case action>`)
}

func TestRejectsMissingScope(t *testing.T) {
	wantErr(t, doc(op(obj{"x-infi-scope": nil})), "x-infi-scope")
}

func TestRejectsTwoTags(t *testing.T) {
	wantErr(t, doc(op(obj{"tags": []any{"payments", "usage"}})), "exactly one tag")
}

func TestRejectsTagOutsidePattern(t *testing.T) {
	wantErr(t, doc(op(obj{"tags": []any{"Payments"}})), "does not match snake_case")
}

func TestRejectsMissingOperationID(t *testing.T) {
	wantErr(t, doc(op(obj{"operationId": nil})), "missing operationId")
}

func TestRejectsDuplicateAndMissingFacade(t *testing.T) {
	d := obj{"paths": obj{"/a": obj{"get": op(nil)}, "/b": obj{"get": op(nil)}}}
	wantErr(t, d, "duplicate operationId")
	wantErr(t, doc(op(obj{"x-infi-facade": nil})), "x-infi-facade")
}

func TestRejectsContractWithoutOperations(t *testing.T) {
	wantErr(t, obj{"paths": obj{}}, "contract without operations")
}

func TestResponseWithSeveralSchemasUsesHighestStatus(t *testing.T) {
	two := obj{
		"200": obj{"content": obj{"application/json": obj{"schema": obj{"$ref": "#/components/schemas/A"}}}},
		"202": obj{"content": obj{"application/json": obj{"schema": obj{"properties": obj{}}}}},
	}
	d := doc(op(obj{"responses": two}))
	d["components"] = obj{}
	ops, err := collectOps(d)
	if err != nil {
		t.Fatal(err)
	}
	od, err := buildOp(d, ops[0])
	if err != nil {
		t.Fatal(err)
	}
	if od.Response != "PaymentsList202JSONResponseBody" || od.ResponseOf == "" {
		t.Fatalf("response = %q (%q)", od.Response, od.ResponseOf)
	}
}

func TestPathItemAndOperationParamsAndTransportHeaderExclusion(t *testing.T) {
	d := obj{"components": obj{}, "paths": obj{"/v1/x/{id}": obj{
		"parameters": []any{obj{"in": "path", "name": "id", "required": true, "schema": obj{"type": "string"}}},
		"post": op(obj{
			"operationId": "payments_create", "tags": []any{"payments"},
			"parameters": []any{
				obj{"in": "header", "name": "Idempotency-Key", "schema": obj{"type": "string"}},
				obj{"in": "header", "name": "If-Match", "required": true, "schema": obj{"type": "string"}},
				obj{"in": "header", "name": "Accept-Language", "schema": obj{"type": "string"}},
				obj{"in": "query", "name": "limit", "schema": obj{"type": "integer"}},
			},
		}),
	}}}
	ops, err := collectOps(d)
	if err != nil {
		t.Fatal(err)
	}
	od, err := buildOp(d, ops[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(od.Path_) != 1 || od.Path_[0].GoName != "Id" || od.Path_[0].Field() != "string" {
		t.Fatalf("path = %+v", od.Path_)
	}
	if len(od.Headers) != 1 || od.Headers[0].Name != "Accept-Language" || od.Headers[0].Field() != "*string" {
		t.Fatalf("headers = %+v", od.Headers)
	}
	if len(od.Query) != 1 || od.Query[0].Field() != "*int" {
		t.Fatalf("query = %+v", od.Query)
	}
}

func TestPascalEGoName(t *testing.T) {
	if pascal("customer_sessions") != "CustomerSessions" {
		t.Fatal(pascal("customer_sessions"))
	}
	if goName("X-Buyer-Token") != "XBuyerToken" || goName("enrollmentId") != "EnrollmentId" {
		t.Fatal("goName")
	}
}
