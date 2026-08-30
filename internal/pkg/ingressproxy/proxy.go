package ingressproxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/config"
)

type proxyRoute struct {
	domainID string
	path     string
	proxy    *httputil.ReverseProxy
}

type snapshot map[string][]proxyRoute

type proxyServer struct {
	addr   string
	server *http.Server

	mu       sync.Mutex
	byDomain map[string]domainSnapshot
	routes   atomic.Value
}

type domainSnapshot struct {
	hostname string
	routes   []model.DomainRoute
}

func New(cfg config.Config) Proxy {
	p := &proxyServer{
		addr:     cfg.IngressProxyAddr,
		byDomain: make(map[string]domainSnapshot),
	}
	p.routes.Store(snapshot{})
	p.server = &http.Server{Addr: cfg.IngressProxyAddr, Handler: p}
	return p
}

func (p *proxyServer) URL() string {
	return "http://" + p.addr
}

func (p *proxyServer) Start() error {
	listener, err := net.Listen("tcp", p.addr)
	if err != nil {
		return fmt.Errorf("ingressproxy: listen: %w", err)
	}
	go func() {
		_ = p.server.Serve(listener)
	}()
	return nil
}

func (p *proxyServer) Shutdown(ctx context.Context) error {
	return p.server.Shutdown(ctx)
}

func (p *proxyServer) PrepareDomain(domainID, hostname string, oldRoutes, newRoutes []model.DomainRoute) error {
	merged := make([]model.DomainRoute, 0, len(oldRoutes)+len(newRoutes))
	newStripped := make(map[string]struct{})
	for _, route := range newRoutes {
		if route.StripPrefix {
			merged = append(merged, route)
			newStripped[route.Path] = struct{}{}
		}
	}
	for _, route := range oldRoutes {
		if !route.StripPrefix {
			continue
		}
		if _, exists := newStripped[route.Path]; !exists {
			merged = append(merged, route)
		}
	}
	return p.replaceDomain(domainID, hostname, merged)
}

func (p *proxyServer) CommitDomain(domainID, hostname string, routes []model.DomainRoute) error {
	return p.replaceDomain(domainID, hostname, routes)
}

func (p *proxyServer) RollbackDomain(domainID, hostname string, routes []model.DomainRoute) error {
	return p.replaceDomain(domainID, hostname, routes)
}

func (p *proxyServer) RemoveDomain(domainID string) {
	p.mu.Lock()
	delete(p.byDomain, domainID)
	p.rebuildLocked()
	p.mu.Unlock()
}

func (p *proxyServer) replaceDomain(domainID, hostname string, routes []model.DomainRoute) error {
	hostname = normalizeHostname(hostname)
	if hostname == "" {
		return fmt.Errorf("ingressproxy: hostname is required")
	}
	filtered := make([]model.DomainRoute, 0, len(routes))
	for _, route := range routes {
		if !route.StripPrefix {
			continue
		}
		target, err := url.Parse(route.OriginURL)
		if err != nil || target.Scheme == "" || target.Host == "" {
			return fmt.Errorf("ingressproxy: invalid origin URL")
		}
		if sameEndpoint(target.Host, p.addr) {
			return fmt.Errorf("ingressproxy: origin cannot target the ingress proxy")
		}
		filtered = append(filtered, route)
	}

	p.mu.Lock()
	p.byDomain[domainID] = domainSnapshot{hostname: hostname, routes: filtered}
	p.rebuildLocked()
	p.mu.Unlock()
	return nil
}

func (p *proxyServer) rebuildLocked() {
	next := make(snapshot)
	for domainID, domain := range p.byDomain {
		for _, route := range domain.routes {
			target, _ := url.Parse(route.OriginURL)
			reverseProxy := httputil.NewSingleHostReverseProxy(target)
			reverseProxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
				http.Error(w, "bad gateway", http.StatusBadGateway)
			}
			next[domain.hostname] = append(next[domain.hostname], proxyRoute{
				domainID: domainID,
				path:     route.Path,
				proxy:    reverseProxy,
			})
		}
	}
	for hostname := range next {
		sort.Slice(next[hostname], func(i, j int) bool {
			if len(next[hostname][i].path) != len(next[hostname][j].path) {
				return len(next[hostname][i].path) > len(next[hostname][j].path)
			}
			return next[hostname][i].path < next[hostname][j].path
		})
	}
	p.routes.Store(next)
}

func (p *proxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hostname := normalizeHostname(r.Host)
	routes := p.routes.Load().(snapshot)[hostname]
	for _, route := range routes {
		if !matchesPrefix(r.URL.Path, route.path) {
			continue
		}
		path := strings.TrimPrefix(r.URL.Path, route.path)
		if path == "" {
			path = "/"
		}
		r.URL.Path = path
		r.URL.RawPath = ""
		r.Header.Set("X-Forwarded-Host", r.Host)
		if r.Header.Get("X-Forwarded-Proto") == "" {
			if r.TLS == nil {
				r.Header.Set("X-Forwarded-Proto", "http")
			} else {
				r.Header.Set("X-Forwarded-Proto", "https")
			}
		}
		r.Header.Set("X-Forwarded-Prefix", route.path)
		route.proxy.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func matchesPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func normalizeHostname(host string) string {
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func sameEndpoint(left, right string) bool {
	leftHost, leftPort, leftErr := net.SplitHostPort(left)
	rightHost, rightPort, rightErr := net.SplitHostPort(right)
	if leftErr != nil || rightErr != nil || leftPort != rightPort {
		return false
	}
	leftIP := net.ParseIP(leftHost)
	rightIP := net.ParseIP(rightHost)
	leftLoopback := strings.EqualFold(leftHost, "localhost") || leftIP != nil && leftIP.IsLoopback()
	rightLoopback := strings.EqualFold(rightHost, "localhost") || rightIP != nil && rightIP.IsLoopback()
	if leftLoopback && rightLoopback {
		return true
	}
	return leftIP != nil && rightIP != nil && leftIP.Equal(rightIP)
}
