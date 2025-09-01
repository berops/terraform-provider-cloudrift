// The api.json is downloaded from https://api.cloudrift.ai/api-docs/openapi2.json (19th August, 2025)
// To generate a go client from the spec several changes had to be made to the file, including changes
// to the described API.
//
// The API had to be translated to the openapi version 3.0.4
// Removed `"items": false`, fields
// Removed IpRangeSelector as there was no such type in the API, for endpoint ListIpRangesRequestProto.
// Removed ProviderSelector as there was no such type in the API, for endpoint ListIpRangesRequestProto.
// Replaced nullable types via the `oneOf` directive, by having `"type": "...", "nullable": true`
//
// several endpoints seems to return code 200 on success, but the API describes the returns as 201
//
// - Instances
// - InstanceTypes
// - Recipes
//
// Similarly for the InstanceUserInstructions.instruction_template, changed type "array" to "string" to reflect the actuall implementation.
//
// The instance-types list return type was adjusted to instaluce the InstanceVariantInfo as a reference which was missing.
//
// The generated file is mostly used for its types. A custom client has been created for interaction with the CloudRift API
// that leverages the generated types.
//
//go:generate go tool oapi-codegen -generate=types,client -package cloudriftapi -o cloudriftapi.gen.go api.json
package cloudriftapi
