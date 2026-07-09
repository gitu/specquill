package importer

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// importOpenAPI fetches an OpenAPI/Swagger document (JSON or YAML) and produces
// two files: the raw spec (openapi.yaml) and a readable index.md summarizing the
// info block, every path+operation, and the component schema names — the form
// the copilot actually grounds on.
func importOpenAPI(ctx context.Context, hc *http.Client, src Source) (map[string]string, error) {
	body, err := get(ctx, hc, src.Remote, src.Token, "application/json, application/yaml, text/yaml")
	if err != nil {
		return nil, err
	}
	// YAML is a superset of JSON, so one unmarshal handles both wire formats.
	var doc map[string]any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi spec: %w", err)
	}
	if doc["openapi"] == nil && doc["swagger"] == nil {
		return nil, fmt.Errorf("%s is not an OpenAPI/Swagger document", src.Remote)
	}

	var b strings.Builder
	info, _ := doc["info"].(map[string]any)
	title := str(info, "title", src.Name)
	fmt.Fprintf(&b, "# %s\n\n", title)
	if v := str(info, "version", ""); v != "" {
		fmt.Fprintf(&b, "**Version:** %s  \n", v)
	}
	if d := str(info, "description", ""); d != "" {
		fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(d))
	}

	if paths, ok := doc["paths"].(map[string]any); ok {
		fmt.Fprintf(&b, "\n## Endpoints\n\n")
		for _, p := range sortedKeys(paths) {
			ops, _ := paths[p].(map[string]any)
			for _, m := range sortedKeys(ops) {
				method := strings.ToUpper(m)
				if !isHTTPMethod(method) {
					continue
				}
				op, _ := ops[m].(map[string]any)
				summary := str(op, "summary", str(op, "operationId", ""))
				fmt.Fprintf(&b, "- `%s %s`", method, p)
				if summary != "" {
					fmt.Fprintf(&b, " — %s", summary)
				}
				b.WriteByte('\n')
			}
		}
	}

	if schemas := schemaNames(doc); len(schemas) > 0 {
		fmt.Fprintf(&b, "\n## Schemas\n\n")
		for _, s := range schemas {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}

	return map[string]string{
		"openapi.yaml": string(body),
		"index.md":     b.String(),
	}, nil
}

func schemaNames(doc map[string]any) []string {
	if comp, ok := doc["components"].(map[string]any); ok {
		if s, ok := comp["schemas"].(map[string]any); ok {
			return sortedKeys(s)
		}
	}
	if s, ok := doc["definitions"].(map[string]any); ok { // swagger 2.0
		return sortedKeys(s)
	}
	return nil
}

func isHTTPMethod(m string) bool {
	switch m {
	case "GET", "PUT", "POST", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE":
		return true
	}
	return false
}

func str(m map[string]any, key, def string) string {
	if m == nil {
		return def
	}
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
