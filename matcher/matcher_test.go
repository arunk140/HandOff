package matcher

import (
	"testing"

	"handoff/config"
)

func TestMatchGlob(t *testing.T) {
	routes := []config.Route{
		{Path: "/api/**", Backend: "https://api.example.com"},
		{Path: "/users/*", Backend: "https://users.example.com"},
		{Path: "/exact/path", Backend: "https://exact.example.com"},
	}

	m := New(routes)

	tests := []struct {
		path   string
		match  bool
		backend string
	}{
		{"/api/users", true, "https://api.example.com"},
		{"/api/users/123", true, "https://api.example.com"},
		{"/api/", true, "https://api.example.com"},
		{"/api", true, "https://api.example.com"},
		{"/users/123", true, "https://users.example.com"},
		{"/users/", false, ""},
		{"/users/123/extra", false, ""},
		{"/exact/path", true, "https://exact.example.com"},
		{"/exact/path/", true, "https://exact.example.com"},
		{"/exact/path/extra", false, ""},
		{"/other", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			route := m.Match(tt.path, "GET")
			if tt.match && route == nil {
				t.Errorf("expected match for %s, got nil", tt.path)
			}
			if !tt.match && route != nil {
				t.Errorf("expected no match for %s, got %s", tt.path, route.Backend)
			}
			if route != nil && route.Backend != tt.backend {
				t.Errorf("expected backend %s, got %s", tt.backend, route.Backend)
			}
		})
	}
}

func TestMatchRegex(t *testing.T) {
	routes := []config.Route{
		{Path: "~/users/\\d+$", Backend: "https://api.example.com"},
		{Path: "~/products/[a-z]+$", Backend: "https://products.example.com"},
	}

	m := New(routes)

	tests := []struct {
		path  string
		match bool
	}{
		{"/users/123", true},
		{"/users/abc", false},
		{"/users/123/extra", false},
		{"/products/foo", true},
		{"/products/123", false},
		{"/other", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			route := m.Match(tt.path, "GET")
			if tt.match && route == nil {
				t.Errorf("expected match for %s, got nil", tt.path)
			}
			if !tt.match && route != nil {
				t.Errorf("expected no match for %s", tt.path)
			}
		})
	}
}

func TestMatchMethod(t *testing.T) {
	routes := []config.Route{
		{Path: "/api/**", Methods: []string{"POST", "PUT"}, Backend: "https://api.example.com"},
		{Path: "/read/**", Methods: []string{"GET"}, Backend: "https://read.example.com"},
	}

	m := New(routes)

	tests := []struct {
		method string
		path   string
		match  bool
	}{
		{"POST", "/api/users", true},
		{"PUT", "/api/users", true},
		{"GET", "/api/users", false},
		{"DELETE", "/api/users", false},
		{"get", "/read/data", true},
		{"GET", "/read/data", true},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			route := m.Match(tt.path, tt.method)
			if tt.match && route == nil {
				t.Errorf("expected match for %s %s, got nil", tt.method, tt.path)
			}
			if !tt.match && route != nil {
				t.Errorf("expected no match for %s %s", tt.method, tt.path)
			}
		})
	}
}

func TestMatchAllMethodsWhenEmpty(t *testing.T) {
	routes := []config.Route{
		{Path: "/api/**", Methods: nil, Backend: "https://api.example.com"},
	}

	m := New(routes)

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
		route := m.Match("/api/data", method)
		if route == nil {
			t.Errorf("expected match for %s when methods is empty/nil", method)
		}
	}
}

func TestMatchFirstMatchingRoute(t *testing.T) {
	routes := []config.Route{
		{Path: "/api/**", Backend: "https://first.example.com"},
		{Path: "/api/special", Backend: "https://second.example.com"},
	}

	m := New(routes)

	route := m.Match("/api/special", "GET")
	if route == nil {
		t.Fatal("expected a match")
	}
	if route.Backend != "https://first.example.com" {
		t.Errorf("expected first matching route, got %s", route.Backend)
	}
}
