package domainservice

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/cloudflare"
	domainrequest "tunnelmanager/internal/pkg/request/domain"

	"github.com/google/uuid"
)

const (
	maxDomainRoutes = 50
	maxRoutePathLen = 256
)

func normalizeRoutes(domainID string, inputs []domainrequest.RouteInput, now time.Time) ([]model.DomainRoute, string, error) {
	return normalizeRoutesWithLegacyPaths(domainID, inputs, now, false)
}

func normalizeRoutesWithLegacyPaths(domainID string, inputs []domainrequest.RouteInput, now time.Time, allowDirectOriginPath bool) ([]model.DomainRoute, string, error) {
	if len(inputs) == 0 || len(inputs) > maxDomainRoutes {
		return nil, "", fmt.Errorf("%w: routes must contain between 1 and %d items", ErrInvalidRoutes, maxDomainRoutes)
	}

	routes := make([]model.DomainRoute, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	defaultOrigin := ""
	for _, input := range inputs {
		path, err := normalizeRoutePath(input.Path)
		if err != nil {
			return nil, "", err
		}
		if _, exists := seen[path]; exists {
			return nil, "", fmt.Errorf("%w: duplicate path %q", ErrInvalidRoutes, path)
		}
		seen[path] = struct{}{}
		if path == "/" && input.StripPrefix {
			return nil, "", fmt.Errorf("%w: root route cannot strip its prefix", ErrInvalidRoutes)
		}
		originURL, err := normalizeOriginURL(input.OriginURL, input.StripPrefix, allowDirectOriginPath)
		if err != nil {
			return nil, "", err
		}
		if path == "/" {
			defaultOrigin = originURL
		}
		routes = append(routes, model.DomainRoute{
			ID:          uuid.NewString(),
			DomainID:    domainID,
			Path:        path,
			OriginURL:   originURL,
			StripPrefix: input.StripPrefix,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	if defaultOrigin == "" {
		return nil, "", fmt.Errorf("%w: exactly one root route is required", ErrInvalidRoutes)
	}

	sort.Slice(routes, func(i, j int) bool {
		if len(routes[i].Path) != len(routes[j].Path) {
			return len(routes[i].Path) > len(routes[j].Path)
		}
		return routes[i].Path < routes[j].Path
	})
	return routes, defaultOrigin, nil
}

func normalizePersistedRoutes(domainID string, persisted []model.DomainRoute) ([]model.DomainRoute, error) {
	normalized, _, err := normalizeRoutesWithLegacyPaths(domainID, routeInputs(persisted), time.Now().UTC(), true)
	if err != nil {
		return nil, err
	}
	metadata := make(map[string]model.DomainRoute, len(persisted))
	for _, route := range persisted {
		metadata[route.Path] = route
	}
	for i := range normalized {
		if original, ok := metadata[normalized[i].Path]; ok {
			normalized[i].ID = original.ID
			normalized[i].CreatedAt = original.CreatedAt
			normalized[i].UpdatedAt = original.UpdatedAt
		}
	}
	return normalized, nil
}

func normalizeRoutePath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return "", fmt.Errorf("%w: path %q must be an absolute path without query or fragment", ErrInvalidRoutes, raw)
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: path contains control characters", ErrInvalidRoutes)
		}
	}
	if !utf8.ValidString(path) || len(path) > maxRoutePathLen {
		return "", fmt.Errorf("%w: path must be valid UTF-8 and at most %d bytes", ErrInvalidRoutes, maxRoutePathLen)
	}
	if path != "/" {
		path = strings.TrimRight(path, "/")
	}
	return path, nil
}

func normalizeOriginURL(raw string, stripPrefix, allowDirectOriginPath bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: origin URL must be absolute HTTP or HTTPS", ErrInvalidRoutes)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: origin URL cannot contain credentials, query, or fragment", ErrInvalidRoutes)
	}
	if parsed.Path != "" && parsed.Path != "/" && (stripPrefix || !allowDirectOriginPath) {
		return "", fmt.Errorf("%w: origin URL cannot contain a non-root path", ErrInvalidRoutes)
	}
	if parsed.Path == "/" {
		parsed.Path = ""
		parsed.RawPath = ""
	}
	return parsed.String(), nil
}

func (s *domainService) cloudflareRules(routes []model.DomainRoute) []cloudflare.IngressRule {
	rules := make([]cloudflare.IngressRule, 0, len(routes))
	for _, route := range routes {
		path := ""
		if route.Path != "/" {
			path = "^" + regexp.QuoteMeta(route.Path) + "(/.*)?$"
		}
		service := route.OriginURL
		if route.StripPrefix {
			service = s.proxy.URL()
		}
		rules = append(rules, cloudflare.IngressRule{Path: path, Service: service})
	}
	return rules
}

func routeInputs(routes []model.DomainRoute) []domainrequest.RouteInput {
	inputs := make([]domainrequest.RouteInput, 0, len(routes))
	for _, route := range routes {
		inputs = append(inputs, domainrequest.RouteInput{
			Path:        route.Path,
			OriginURL:   route.OriginURL,
			StripPrefix: route.StripPrefix,
		})
	}
	return inputs
}
