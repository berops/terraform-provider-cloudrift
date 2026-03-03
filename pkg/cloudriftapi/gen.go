// Regeneration workflow:
//
//  1. Download the raw spec:
//     curl -s https://api.cloudrift.ai/api-docs/openapi2.json > api_raw.json
//
//  2. Patch the spec (fixes oapi-codegen incompatibilities):
//     go run patchspec.go
//
//  3. Generate the Go client:
//     go generate
//
// The patchspec.go tool applies the following transformations to api_raw.json → api.json:
//   - Downgrade OpenAPI 3.1.0 → 3.0.4 (oapi-codegen does not support 3.1.0)
//   - Replace "items": false with "items": {} (oapi-codegen cannot parse boolean items)
//   - Convert 3.1.0 nullable types ["type", "null"] → 3.0.x "nullable": true
//   - Convert oneOf with {"type":"null"} → 3.0.x nullable
//   - Fix response codes 201 → 200 (API actually returns 200)
//
// The generated file is mostly used for its types. A custom client has been created
// for interaction with the CloudRift API that leverages the generated types.
//
//go:generate go tool oapi-codegen -generate=types,client -package cloudriftapi -o cloudriftapi.gen.go api.json
package cloudriftapi
