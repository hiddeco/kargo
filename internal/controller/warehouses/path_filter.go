package warehouses

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gobwas/glob"
)

const (
	prefixPathPrefix = "prefix:"
	globPathPrefix = "glob:"
	regexPathPrefix = "regex:"
	regexpPathPrefix = "regexp:"
)

type pathFilter interface {
	Matches(string) bool
}

type pathFilters []pathFilter

func (p pathFilters) Matches(s string) bool {
	for _, f := range p {
		if f.Matches(s) {
			return true
		}
	}
	return false
}

func newPathFilter(pattern string) (pathFilter, error) {
	switch {
	case strings.HasPrefix(pattern, prefixPathPrefix):
		pattern = strings.TrimPrefix(pattern, prefixPathPrefix)
		return &prefixFilter{
			prefix: pattern,
		}, nil
	case strings.HasPrefix(pattern, globPathPrefix):
		pattern = strings.TrimPrefix(pattern, globPathPrefix)
		g, err := glob.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("syntax error in glob pattern %q: %w", pattern, err)
		}
		return &globPathFilter{
			glob: g,
		}, nil
	case strings.HasPrefix(pattern, regexPathPrefix):
		pattern = strings.TrimPrefix(pattern, regexPathPrefix)
		r, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("syntax error in regex pattern %q: %w", pattern, err)
		}
		return &regexPathFilter{
			regexp: r,
		}, nil
	case strings.HasPrefix(pattern, regexpPathPrefix):
		pattern = strings.TrimPrefix(pattern, regexpPathPrefix)
		r, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("syntax error in regex pattern %q: %w", pattern, err)
		}
		return &regexPathFilter{
			regexp: r,
		}, nil
	default:
		pattern = strings.TrimPrefix(pattern, prefixPathPrefix)
		return &prefixFilter{
			prefix: pattern,
		}, nil
	}
}

type prefixFilter struct {
	prefix string
}

func (p *prefixFilter) Matches(s string) bool {
	return strings.HasPrefix(s, p.prefix)
}

type globPathFilter struct {
	glob glob.Glob
}

func (g *globPathFilter) Matches(s string) bool {
	if g.glob == nil {
		return false
	}
	return g.glob.Match(s)
}

type regexPathFilter struct {
	regexp *regexp.Regexp
}

func (r *regexPathFilter) Matches(s string) bool {
	if r.regexp == nil {
		return false
	}
	return r.regexp.MatchString(s)
}
