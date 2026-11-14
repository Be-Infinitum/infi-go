package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Fixed order of HTTP methods within a path (like METHODS in gen-facade.ts).
var httpMethods = []string{"get", "put", "post", "delete", "patch", "options", "head"}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

type obj = map[string]any

// Op is an already validated contract operation.
type Op struct {
	OperationID string
	Tag         string
	Action      string
	Method      string // GET, POST...
	Path        string
	Scope       string
	Summary     string

	raw  obj // the operation in the contract
	item obj // the path item (shared parameters)
}

func parseDoc(raw []byte) (obj, error) {
	var doc obj
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("invalid contract: %w", err)
	}
	return doc, nil
}

// pascal converts snake_case to PascalCase: get_wallet -> GetWallet.
func pascal(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		if r == '_' {
			up = true
			continue
		}
		if up {
			b.WriteString(strings.ToUpper(string(r)))
			up = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// collectOps validates the contract and returns the operations. The error messages
// are those of collectOps in gen-facade.ts.
func collectOps(doc obj) ([]Op, error) {
	var ops []Op
	seen := map[string]bool{}
	paths, _ := doc["paths"].(obj)
	keys := make([]string, 0, len(paths))
	for p := range paths {
		keys = append(keys, p)
	}
	sort.Strings(keys) // Go's JSON does not keep order; this keeps the output deterministic.
	for _, path := range keys {
		item, _ := paths[path].(obj)
		for _, m := range httpMethods {
			op, ok := item[m].(obj)
			if !ok || op == nil {
				continue
			}
			where := fmt.Sprintf("%s %s", strings.ToUpper(m), path)
			id, _ := op["operationId"].(string)
			if id == "" {
				return nil, fmt.Errorf("%s: missing operationId", where)
			}
			tags, _ := op["tags"].([]any)
			tag, isStr := "", false
			if len(tags) == 1 {
				tag, isStr = tags[0].(string)
			}
			if len(tags) != 1 || !isStr {
				return nil, fmt.Errorf("%s (%s): needs exactly one tag, has %d", where, id, len(tags))
			}
			if !namePattern.MatchString(tag) {
				return nil, fmt.Errorf("%s: tag \"%s\" does not match snake_case", where, tag)
			}
			action := ""
			if strings.HasPrefix(id, tag+"_") {
				action = strings.TrimPrefix(id, tag+"_")
			}
			if action == "" || !namePattern.MatchString(action) {
				return nil, fmt.Errorf("%s: operationId \"%s\" must be \"%s_<snake_case action>\"", where, id, tag)
			}
			if seen[id] {
				return nil, fmt.Errorf("%s: duplicate operationId \"%s\"", where, id)
			}
			seen[id] = true
			if f, _ := op["x-infi-facade"].(bool); !f {
				return nil, fmt.Errorf("%s (%s): missing x-infi-facade: true", where, id)
			}
			scope, _ := op["x-infi-scope"].(string)
			if scope == "" {
				return nil, fmt.Errorf("%s (%s): missing x-infi-scope", where, id)
			}
			summary, _ := op["summary"].(string)
			ops = append(ops, Op{
				OperationID: id, Tag: tag, Action: action, Method: strings.ToUpper(m),
				Path: path, Scope: scope, Summary: strings.Join(strings.Fields(summary), " "),
				raw: op, item: item,
			})
		}
	}
	if len(ops) == 0 {
		return nil, errors.New("contract without operations")
	}
	return ops, nil
}
