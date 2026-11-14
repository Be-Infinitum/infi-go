package main

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"
	"text/template"
	"unicode"
)

// Headers the transport handles (CallOptions); they are not part of Headers.
var transportHeaders = map[string]bool{"idempotency-key": true, "if-match": true, "x-infi-channel": true}

type param struct {
	Name     string // name in the contract
	GoName   string
	GoType   string
	Required bool
}

func (p param) Field() string {
	t := p.GoType
	if !p.Required {
		t = "*" + t
	}
	return t
}

type opData struct {
	Op
	Service    string // PascalCase of the tag
	Method_    string // PascalCase of the action
	Prefix     string // OperationId in PascalCase (prefix of the input types)
	Input      string // name of the input struct ("" if the operation has no input)
	Path_      []param
	Query      []param
	Headers    []param
	BodyType   string
	BodyReq    bool
	Response   string // Go type of the 2xx response ("" without a body)
	ResponseOf string // note about the chosen 2xx
}

func (o opData) HasPath() bool    { return len(o.Path_) > 0 }
func (o opData) HasQuery() bool   { return len(o.Query) > 0 }
func (o opData) HasHeaders() bool { return len(o.Headers) > 0 }
func (o opData) HasBody() bool    { return o.BodyType != "" }
func (o opData) Doc() string {
	d := fmt.Sprintf("%s %s (scope %s)", o.Op.Method, o.Op.Path, o.Scope)
	if o.Summary != "" {
		d += ": " + o.Summary
	}
	return d
}
func (o opData) BodyField() string {
	if o.BodyReq {
		return o.BodyType
	}
	return "*" + o.BodyType
}

type tagData struct {
	Name string // PascalCase
	Ops  []opData
}

// ---- contract resolution ----

func components(doc obj, kind string) obj {
	c, _ := doc["components"].(obj)
	m, _ := c[kind].(obj)
	return m
}

// deref follows local $refs (#/components/<kind>/<name>) and also returns the name.
func deref(doc obj, v any) (obj, string, error) {
	m, _ := v.(obj)
	if m == nil {
		return nil, "", fmt.Errorf("expected an object in the contract")
	}
	for i := 0; i < 16; i++ {
		ref, ok := m["$ref"].(string)
		if !ok {
			return m, "", nil
		}
		parts := strings.Split(ref, "/")
		if len(parts) != 4 || parts[0] != "#" || parts[1] != "components" {
			return nil, "", fmt.Errorf("unsupported $ref: %s", ref)
		}
		next, _ := components(doc, parts[2])[parts[3]].(obj)
		if next == nil {
			return nil, "", fmt.Errorf("$ref without target: %s", ref)
		}
		if _, still := next["$ref"]; !still {
			return next, parts[3], nil
		}
		m = next
	}
	return nil, "", fmt.Errorf("$ref cycle")
}

// goName converts a schema/property name into a Go identifier like
// oapi-codegen does (segments separated by non-alphanumerics, first letter uppercase).
func goName(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			up = true
			continue
		}
		if up {
			r = unicode.ToUpper(r)
			up = false
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out != "" && unicode.IsDigit(rune(out[0])) {
		out = "N" + out
	}
	return out
}

// primitive returns the Go type of a parameter (path/query/header), following $ref.
func primitive(doc obj, schema any) (string, error) {
	s, _, err := deref(doc, schema)
	if err != nil {
		return "", err
	}
	t := s["type"]
	if list, ok := t.([]any); ok { // OpenAPI 3.1: ["string","null"]
		t = nil
		for _, e := range list {
			if e != "null" {
				t = e
			}
		}
	}
	switch t {
	case "string":
		return "string", nil
	case "integer":
		return "int", nil
	case "number":
		return "float64", nil
	case "boolean":
		return "bool", nil
	}
	return "", fmt.Errorf("unsupported parameter type: %v", t)
}

func collectParams(doc obj, o Op) (path, query, headers []param, err error) {
	type key struct{ in, name string }
	var order []key
	byKey := map[key]obj{}
	add := func(list any) error {
		items, _ := list.([]any)
		for _, it := range items {
			p, _, err := deref(doc, it)
			if err != nil {
				return err
			}
			name, _ := p["name"].(string)
			in, _ := p["in"].(string)
			k := key{in, name}
			if in == "header" {
				k.name = strings.ToLower(name) // headers are case-insensitive
			}
			if _, dup := byKey[k]; !dup {
				order = append(order, k)
			}
			byKey[k] = p // the operation's wins over the path item's
		}
		return nil
	}
	if err = add(o.item["parameters"]); err != nil {
		return
	}
	if err = add(o.raw["parameters"]); err != nil {
		return
	}
	for _, k := range order {
		p := byKey[k]
		name := p["name"].(string)
		in := p["in"].(string)
		if in == "cookie" {
			err = fmt.Errorf("%s %s (%s): cookie parameter not supported", o.Method, o.Path, o.OperationID)
			return
		}
		if in == "header" && transportHeaders[strings.ToLower(name)] {
			continue
		}
		gt, perr := primitive(doc, p["schema"])
		if perr != nil {
			err = fmt.Errorf("%s %s (%s): parameter %q: %v", o.Method, o.Path, o.OperationID, name, perr)
			return
		}
		req, _ := p["required"].(bool)
		pp := param{Name: name, GoName: goName(name), GoType: gt, Required: req || in == "path"}
		switch in {
		case "path":
			path = append(path, pp)
		case "query":
			query = append(query, pp)
		case "header":
			headers = append(headers, pp)
		}
	}
	return
}

// jsonSchemaType returns the Go type that oapi-codegen gives a body/response schema:
// a $ref to components/schemas/X becomes X; an inline one becomes the type named by
// generate-types-for-anonymous-schemas (<OperationId>[<status>]JSON<Body>).
func schemaType(doc obj, schema any, inlineName string) (string, error) {
	m, _ := schema.(obj)
	if m == nil {
		return "", fmt.Errorf("missing schema")
	}
	if ref, ok := m["$ref"].(string); ok {
		parts := strings.Split(ref, "/")
		if len(parts) != 4 || parts[1] != "components" || parts[2] != "schemas" {
			return "", fmt.Errorf("unsupported $ref: %s", ref)
		}
		if goName(parts[3]) == "Error" {
			// The overlay renames the Error schema to ErrorResponse; infi.Error is the SDK error.
			return "", fmt.Errorf("the Error schema cannot be a response type (collides with infi.Error)")
		}
		return goName(parts[3]), nil
	}
	return inlineName, nil
}

func jsonContent(doc obj, holder obj) (any, bool) {
	c, _ := holder["content"].(obj)
	j, _ := c["application/json"].(obj)
	if j == nil {
		return nil, false
	}
	s, ok := j["schema"]
	return s, ok
}

func buildOp(doc obj, o Op) (opData, error) {
	d := opData{Op: o, Service: pascal(o.Tag), Method_: pascal(o.Action), Prefix: pascal(o.OperationID)}
	where := fmt.Sprintf("%s %s (%s)", o.Method, o.Path, o.OperationID)
	var err error
	d.Path_, d.Query, d.Headers, err = collectParams(doc, o)
	if err != nil {
		return d, err
	}
	opPascal := pascal(o.OperationID)

	if rb, ok := o.raw["requestBody"]; ok {
		body, _, err := deref(doc, rb)
		if err != nil {
			return d, fmt.Errorf("%s: requestBody: %v", where, err)
		}
		if _, ok := jsonContent(doc, body); !ok {
			return d, fmt.Errorf("%s: requestBody without application/json", where)
		}
		d.BodyType = opPascal + "JSONRequestBody"
		d.BodyReq, _ = body["required"].(bool)
	}

	// Response: the 2xx with a JSON body. If several 2xx carry different schemas,
	// the one with the highest status wins (the contract declares them as a superset: the 202 of
	// config_apply = result + previewUrl).
	responses, _ := o.raw["responses"].(obj)
	var statuses []string
	for s := range responses {
		if len(s) == 3 && s[0] == '2' {
			statuses = append(statuses, s)
		}
	}
	sort.Strings(statuses)
	if len(statuses) == 0 {
		return d, fmt.Errorf("%s: no 2xx response", where)
	}
	var types []string
	for _, s := range statuses {
		r, name, err := deref(doc, responses[s])
		if err != nil {
			return d, fmt.Errorf("%s: response %s: %v", where, s, err)
		}
		schema, ok := jsonContent(doc, r)
		if !ok {
			continue
		}
		// Inline schema inside components/responses: oapi-codegen names it
		// after the response (BuyerToken); inline in the operation itself, after the operation.
		inline := opPascal + s + "JSONResponseBody"
		if name != "" {
			inline = goName(name)
		}
		t, err := schemaType(doc, schema, inline)
		if err != nil {
			return d, fmt.Errorf("%s: response %s: %v", where, s, err)
		}
		types = append(types, t)
		d.Response = t
	}
	for _, t := range types {
		if t != d.Response {
			d.ResponseOf = strings.Join(types, " | ")
			break
		}
	}

	if d.HasPath() || d.HasQuery() || d.HasHeaders() || d.HasBody() {
		d.Input = opPascal + "Input"
	}
	return d, nil
}

func generate(doc obj, ops []Op) ([]byte, error) {
	var tags []tagData
	idx := map[string]int{}
	var all []opData
	for _, o := range ops {
		d, err := buildOp(doc, o)
		if err != nil {
			return nil, err
		}
		i, ok := idx[o.Tag]
		if !ok {
			i = len(tags)
			idx[o.Tag] = i
			tags = append(tags, tagData{Name: pascal(o.Tag)})
		}
		tags[i].Ops = append(tags[i].Ops, d)
		all = append(all, d)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Tags []tagData
		Ops  []opData
	}{tags, all}); err != nil {
		return nil, err
	}
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt: %w\n%s", err, buf.String())
	}
	return src, nil
}

var tmpl = template.Must(template.New("resources").Funcs(template.FuncMap{"q": func(s string) string { return fmt.Sprintf("%q", s) }}).Parse(`// Code generated by internal/cmd/genfacade from api/openapi.sdk.json; DO NOT EDIT.

package infi

import "context"

// Resources gathers one service per contract tag (client.<Tag>.<Action>).
type Resources struct {
{{- range .Tags}}
	{{.Name}} *{{.Name}}Service
{{- end}}
}

func newResources(c *Client) Resources {
	return Resources{
{{- range .Tags}}
		{{.Name}}: &{{.Name}}Service{c: c},
{{- end}}
	}
}

{{range .Tags}}
// {{.Name}}Service groups the operations of the tag.
type {{.Name}}Service struct{ c *Client }
{{end}}
{{range .Ops}}
{{- if .Input}}
{{- if .HasPath}}
// {{.Prefix}}Path holds the path parameters of {{.OperationID}}.
type {{.Prefix}}Path struct {
{{- range .Path_}}
	{{.GoName}} {{.Field}} ` + "`path:\"{{.Name}}\"`" + `
{{- end}}
}
{{end}}
{{- if .HasQuery}}
// {{.Prefix}}Query holds the query parameters of {{.OperationID}}.
type {{.Prefix}}Query struct {
{{- range .Query}}
	{{.GoName}} {{.Field}} ` + "`query:\"{{.Name}}\"`" + `
{{- end}}
}
{{end}}
{{- if .HasHeaders}}
// {{.Prefix}}Headers holds the headers of {{.OperationID}} (Idempotency-Key, If-Match and
// X-Infi-Channel live in the call options).
type {{.Prefix}}Headers struct {
{{- range .Headers}}
	{{.GoName}} {{.Field}} ` + "`header:\"{{.Name}}\"`" + `
{{- end}}
}
{{end}}
// {{.Input}} gathers the parameters of {{.OperationID}}.
type {{.Input}} struct {
{{- if .HasPath}}
	Path {{.Prefix}}Path
{{- end}}
{{- if .HasQuery}}
	Query {{.Prefix}}Query
{{- end}}
{{- if .HasHeaders}}
	Headers {{.Prefix}}Headers
{{- end}}
{{- if .HasBody}}
	Body {{.BodyField}}
{{- end}}
}
{{end}}
// {{.Doc}}
{{- if .ResponseOf}}
//
// 2xx responses with different schemas ({{.ResponseOf}}): the response is decoded into {{.Response}}.
{{- end}}
func (s *{{.Service}}Service) {{.Method_}}(ctx context.Context,{{if .Input}} in {{.Input}},{{end}} opts ...CallOption) ({{if .Response}}*{{.Response}}, {{end}}error) {
	req := request{method: {{q .Op.Method}}, path: {{q .Op.Path}}
{{- if .HasPath}}, pathParams: in.Path{{end}}
{{- if .HasQuery}}, query: in.Query{{end}}
{{- if .HasHeaders}}, headers: in.Headers{{end}}
{{- if .HasBody}}, body: in.Body{{end}}}
{{- if .Response}}
	var out {{.Response}}
	if err := s.c.do(ctx, req, opts, &out); err != nil {
		return nil, err
	}
	return &out, nil
{{- else}}
	return s.c.do(ctx, req, opts, nil)
{{- end}}
}
{{end}}
// Operation describes a contract operation, for the reachability test.
type Operation struct {
	OperationID string
	Tag         string
	// Service is the client field (client.<Service>) and Method is the service method.
	Service    string
	Method     string
	HTTPMethod string
	Path       string
	Scope      string
}

// Operations lists the contract operations.
var Operations = []Operation{
{{- range .Ops}}
	{OperationID: {{q .OperationID}}, Tag: {{q .Tag}}, Service: {{q .Service}}, Method: {{q .Method_}}, HTTPMethod: {{q .Op.Method}}, Path: {{q .Op.Path}}, Scope: {{q .Scope}}},
{{- end}}
}
`))
