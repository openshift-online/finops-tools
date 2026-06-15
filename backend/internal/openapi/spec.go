// Package openapi embeds the finops-backend OpenAPI specification.
package openapi

import _ "embed"

// YAML is the embedded OpenAPI 3.0 document served at GET /openapi.yaml.
//
//go:embed openapi.yaml
var YAML []byte
