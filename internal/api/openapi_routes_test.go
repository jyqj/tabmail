package api

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/outbound"
)

// specOnlyRoutes lists spec paths that intentionally have no chi route, e.g.
// endpoints served by a reverse proxy. Keep empty unless there is a reason.
var specOnlyRoutes = map[string]bool{}

// routeOnlyRoutes lists chi routes that are intentionally undocumented.
var routeOnlyRoutes = map[string]bool{}

// TestOpenAPIMatchesRoutes fails when internal/api/openapi.yaml drifts from the
// routes actually registered by NewRouter, in either direction.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	specEndpoints, err := parseSpecEndpoints(mustReadSpec(t))
	if err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	routeEndpoints := registeredEndpoints(t)

	var missingInSpec, missingInRouter []string
	for ep := range routeEndpoints {
		if !specEndpoints[ep] && !routeOnlyRoutes[ep] {
			missingInSpec = append(missingInSpec, ep)
		}
	}
	for ep := range specEndpoints {
		if !routeEndpoints[ep] && !specOnlyRoutes[ep] {
			missingInRouter = append(missingInRouter, ep)
		}
	}
	sort.Strings(missingInSpec)
	sort.Strings(missingInRouter)

	if len(missingInSpec) > 0 {
		t.Errorf("routes registered but absent from openapi.yaml:\n  %s", strings.Join(missingInSpec, "\n  "))
	}
	if len(missingInRouter) > 0 {
		t.Errorf("openapi.yaml documents endpoints with no route:\n  %s", strings.Join(missingInRouter, "\n  "))
	}
}

func mustReadSpec(t *testing.T) string {
	t.Helper()
	data, err := openapiSpec.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("read embedded spec: %v", err)
	}
	return string(data)
}

// registeredEndpoints walks the real router and returns "METHOD path" keys.
// The router is built with zero-valued dependencies: NewRouter only stores
// them on handlers, so nothing is dereferenced during registration.
func registeredEndpoints(t *testing.T) map[string]bool {
	t.Helper()
	handler := NewRouter(RouterConfig{
		// A non-nil outbound service is required for the /send routes to register.
		OutboundService: outbound.NewService(config.Outbound{}, nil, zerolog.Nop()),
	})
	routes, ok := handler.(chi.Routes)
	if !ok {
		t.Fatalf("router is %T, want chi.Routes", handler)
	}

	endpoints := map[string]bool{}
	walk := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		endpoints[endpointKey(method, route)] = true
		return nil
	}
	if err := chi.Walk(routes, walk); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return endpoints
}

func endpointKey(method, path string) string {
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return strings.ToUpper(method) + " " + path
}

var specMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true,
	"delete": true, "head": true, "options": true,
}

// parseSpecEndpoints extracts "METHOD path" keys from the `paths:` section of
// the OpenAPI document. The spec is hand-written with a fixed two-space
// indent per level, so a scanner avoids pulling in a YAML dependency.
func parseSpecEndpoints(spec string) (map[string]bool, error) {
	endpoints := map[string]bool{}
	inPaths := false
	currentPath := ""

	for _, line := range strings.Split(spec, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			inPaths = strings.HasPrefix(line, "paths:")
			currentPath = ""
			continue
		}
		if !inPaths {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, _, hasColon := strings.Cut(strings.TrimSpace(line), ":")
		if !hasColon {
			continue
		}
		switch indent {
		case 2:
			currentPath = strings.Trim(key, `"'`)
		case 4:
			if currentPath != "" && specMethods[strings.ToLower(key)] {
				endpoints[endpointKey(key, currentPath)] = true
			}
		}
	}

	if len(endpoints) == 0 {
		return nil, errNoSpecPaths
	}
	return endpoints, nil
}

var errNoSpecPaths = errSpec("no paths found in openapi.yaml")

type errSpec string

func (e errSpec) Error() string { return string(e) }
