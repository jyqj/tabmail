package api

import (
	"fmt"
	"regexp"
)

// Small independently reviewed path fragments keep transport recovery separate
// from the company API. /openapi.yaml still serves ONE complete specification.
func openAPIDocument() ([]byte, error) {
	base, err := openapiSpec.ReadFile("openapi.yaml")
	if err != nil {
		return nil, err
	}
	fragment, err := openapiSpec.ReadFile("ingress-paths.yaml")
	if err != nil {
		return nil, err
	}
	roots := regexp.MustCompile(`(?m)^[A-Za-z][A-Za-z0-9_]*:`).FindAllString(string(base), -1)
	if len(roots) == 0 || roots[len(roots)-1] != "paths:" {
		return nil, fmt.Errorf("OpenAPI paths must be the final root mapping before appending path fragments")
	}
	return append(append(base, '\n'), fragment...), nil
}
