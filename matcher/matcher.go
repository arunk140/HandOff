package matcher

import (
	"path/filepath"
	"regexp"
	"strings"

	"handoff/config"
)

type Matcher struct {
	routes []config.Route
}

func New(routes []config.Route) *Matcher {
	return &Matcher{routes: routes}
}

func (m *Matcher) Match(path, method string) *config.Route {
	for i := range m.routes {
		route := &m.routes[i]
		if !matchMethod(route.Methods, method) {
			continue
		}
		if !matchPath(route.Path, path) {
			continue
		}
		return route
	}
	return nil
}

func matchMethod(allowed []string, method string) bool {
	if len(allowed) == 0 {
		return true
	}
	method = strings.ToUpper(method)
	for _, m := range allowed {
		if strings.ToUpper(m) == method {
			return true
		}
	}
	return false
}

func matchPath(pattern, path string) bool {
	if strings.HasPrefix(pattern, "~") {
		re, err := regexp.Compile(pattern[1:])
		if err != nil {
			return false
		}
		return re.MatchString(path)
	}
	return matchGlob(pattern, path)
}

func matchGlob(pattern, path string) bool {
	patSegs := splitPath(pattern)
	pathSegs := splitPath(path)
	return matchSegments(patSegs, pathSegs)
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return []string{}
	}
	return strings.Split(p, "/")
}

func matchSegments(pat, path []string) bool {
	pi, si := 0, 0
	for pi < len(pat) {
		if pat[pi] == "**" {
			pi++
			if pi == len(pat) {
				return true
			}
			for si < len(path) {
				if matchSegments(pat[pi:], path[si:]) {
					return true
				}
				si++
			}
			return false
		}
		if si >= len(path) {
			return false
		}
		if matched, _ := filepath.Match(pat[pi], path[si]); !matched {
			return false
		}
		pi++
		si++
	}
	return si == len(path)
}
