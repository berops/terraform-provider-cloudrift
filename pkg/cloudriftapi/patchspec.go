//go:build ignore

// patchspec patches api_raw.json so oapi-codegen can process it.
//
// The CloudRift API spec is OpenAPI 3.1.0, but oapi-codegen v2 only supports 3.0.x.
// This tool applies the following transformations:
//
//   - Downgrade openapi version from 3.1.0 to 3.0.4
//   - Replace "items": false with "items": {} (invalid for oapi-codegen's parser)
//   - Convert 3.1.0 nullable types ["type", "null"] to 3.0.x "nullable": true
//   - Convert oneOf with {"type":"null"} entries to 3.0.x nullable
//   - Fix response codes 201 → 200 (API actually returns 200)
//
// Usage: go run patchspec.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	raw, err := os.ReadFile("api_raw.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading api_raw.json: %v\n", err)
		os.Exit(1)
	}

	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		fmt.Fprintf(os.Stderr, "parsing api_raw.json: %v\n", err)
		os.Exit(1)
	}

	// Downgrade openapi version.
	spec["openapi"] = "3.0.4"

	// Patch schemas and paths recursively.
	patchRecursive(spec)

	// Fix response codes 201 → 200.
	fixResponseCodes(spec)

	// Fix instructions_template: spec says array of int32, API returns a base64 string.
	fixInstructionsTemplate(spec)

	out, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshaling patched spec: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile("api.json", out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "writing api.json: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Patched api_raw.json → api.json")
}

// patchRecursive walks the JSON tree and fixes nullable types and "items": false.
func patchRecursive(v any) {
	switch val := v.(type) {
	case map[string]any:
		// Fix "items": false → "items": {}.
		if items, ok := val["items"]; ok {
			if b, isBool := items.(bool); isBool && !b {
				val["items"] = map[string]any{}
			}
		}

		// Fix nullable type arrays: ["string", "null"] → "string" + "nullable": true.
		if t, ok := val["type"]; ok {
			if arr, isArr := t.([]any); isArr {
				nonNull := make([]string, 0, len(arr))
				hasNull := false
				for _, item := range arr {
					s, _ := item.(string)
					if s == "null" {
						hasNull = true
					} else if s != "" {
						nonNull = append(nonNull, s)
					}
				}
				if hasNull && len(nonNull) == 1 {
					val["type"] = nonNull[0]
					val["nullable"] = true
				}
			}
		}

		// Fix oneOf containing {"type":"null"}: collapse to single schema + nullable.
		if oneOf, ok := val["oneOf"]; ok {
			if arr, isArr := oneOf.([]any); isArr {
				var nonNull []any
				hasNull := false
				for _, item := range arr {
					m, isMap := item.(map[string]any)
					if isMap {
						if t, _ := m["type"].(string); t == "null" {
							hasNull = true
							continue
						}
					}
					nonNull = append(nonNull, item)
				}
				if hasNull && len(nonNull) == 1 {
					// Replace oneOf with the single non-null schema.
					delete(val, "oneOf")
					if m, isMap := nonNull[0].(map[string]any); isMap {
						for k, v := range m {
							val[k] = v
						}
					}
					val["nullable"] = true
				} else if hasNull {
					val["oneOf"] = nonNull
					val["nullable"] = true
				}
			}
		}

		// Recurse into all values.
		for _, child := range val {
			patchRecursive(child)
		}

	case []any:
		for _, child := range val {
			patchRecursive(child)
		}
	}
}

// fixInstructionsTemplate changes InstanceUserInstructions.instructions_template
// from array of int32 to string. The API actually returns a base64-encoded string.
func fixInstructionsTemplate(spec map[string]any) {
	schemas, ok := spec["components"].(map[string]any)["schemas"].(map[string]any)
	if !ok {
		return
	}
	iui, ok := schemas["InstanceUserInstructions"].(map[string]any)
	if !ok {
		return
	}
	props, ok := iui["properties"].(map[string]any)
	if !ok {
		return
	}
	if it, ok := props["instructions_template"].(map[string]any); ok {
		it["type"] = "string"
		delete(it, "items")
		delete(it, "format")
		delete(it, "minimum")
	}
}

// fixResponseCodes changes 201 → 200 for endpoints that actually return 200.
// The CloudRift API returns 200 for most endpoints, but the spec declares 201.
// Some endpoints (like /ssh-keys/add) genuinely return 201 and are left as-is.
func fixResponseCodes(spec map[string]any) {
	// Endpoints that genuinely return 201 — do not change these.
	skip := map[string]bool{
		"/api/v1/ssh-keys/add": true,
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return
	}
	for path, methods := range paths {
		if skip[path] {
			continue
		}
		methodMap, ok := methods.(map[string]any)
		if !ok {
			continue
		}
		for _, details := range methodMap {
			detailMap, ok := details.(map[string]any)
			if !ok {
				continue
			}
			responses, ok := detailMap["responses"].(map[string]any)
			if !ok {
				continue
			}
			if _, has200 := responses["200"]; !has200 {
				if val, has201 := responses["201"]; has201 {
					responses["200"] = val
					delete(responses, "201")
				}
			}
		}
	}
}
