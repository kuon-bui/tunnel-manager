// Command rewrite-origins rewrites the host part of managed domains' route
// origin URLs through the tunnel-manager HTTP API.
//
// Typical use when moving the backend to another machine while origins stay
// on the old one: replace localhost origins with the old machine's LAN IP.
// Exclude domains whose origin is the backend itself, since it moves too.
//
//	TM_PASSWORD=... go run ./cmd/rewrite-origins -api http://10.10.1.10:8080 -to 10.10.1.10 -exclude api.example.com
//	TM_PASSWORD=... go run ./cmd/rewrite-origins -api http://10.10.1.10:8080 -to 10.10.1.10 -exclude api.example.com -apply
//	TM_PASSWORD=... go run ./cmd/rewrite-origins -api http://10.10.1.10:8080 -restore origins-backup-....json -apply
//
// Without -apply it only prints the planned changes.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type route struct {
	Path        string `json:"path"`
	OriginURL   string `json:"originUrl"`
	StripPrefix bool   `json:"stripPrefix"`
}

type domain struct {
	ID        string  `json:"id"`
	Hostname  string  `json:"hostname"`
	OriginURL string  `json:"originUrl"`
	Routes    []route `json:"routes"`
}

// change holds the full route list before and after the rewrite, because the
// routes endpoint replaces every route of a domain at once.
type change struct {
	ID       string  `json:"id"`
	Hostname string  `json:"hostname"`
	Old      []route `json:"old"`
	New      []route `json:"new"`
}

type options struct {
	api, user, from, to, only, exclude, restore string
	apply                                       bool
}

func main() {
	var o options
	flag.StringVar(&o.api, "api", "http://localhost:8080", "backend base URL")
	flag.StringVar(&o.user, "user", "admin", "admin username (password from TM_PASSWORD, or set TM_TOKEN)")
	flag.StringVar(&o.from, "from", "localhost,127.0.0.1,::1,0.0.0.0", "comma-separated origin hosts to replace")
	flag.StringVar(&o.to, "to", "", "new origin host, e.g. the old machine's LAN IP")
	flag.StringVar(&o.only, "only", "", "comma-separated public hostnames to limit the change to")
	flag.StringVar(&o.exclude, "exclude", "", "comma-separated public hostnames to leave untouched")
	flag.BoolVar(&o.apply, "apply", false, "send the updates (default: dry run)")
	flag.StringVar(&o.restore, "restore", "", "backup file to restore old routes from")
	flag.Parse()

	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if o.restore == "" && o.to == "" {
		return errors.New("-to is required (or use -restore)")
	}
	c := &client{base: strings.TrimRight(o.api, "/"), http: &http.Client{Timeout: 30 * time.Second}}
	c.token = os.Getenv("TM_TOKEN")
	if c.token == "" {
		password := os.Getenv("TM_PASSWORD")
		if password == "" {
			return errors.New("set TM_PASSWORD (or TM_TOKEN)")
		}
		if err := c.login(o.user, password); err != nil {
			return err
		}
	}

	var changes []change
	if o.restore != "" {
		backup, err := readBackup(o.restore)
		if err != nil {
			return err
		}
		for _, ch := range backup {
			changes = append(changes, change{ID: ch.ID, Hostname: ch.Hostname, Old: ch.New, New: ch.Old})
		}
	} else {
		domains, err := c.listDomains()
		if err != nil {
			return err
		}
		changes, err = planChanges(domains, splitSet(o.from), o.to, splitSet(o.only), splitSet(o.exclude))
		if err != nil {
			return err
		}
	}

	if len(changes) == 0 {
		fmt.Println("nothing to change")
		return nil
	}
	for _, ch := range changes {
		fmt.Println(ch.Hostname)
		for i := range ch.New {
			if ch.Old[i].OriginURL != ch.New[i].OriginURL {
				fmt.Printf("  %-12s %s -> %s\n", ch.New[i].Path, ch.Old[i].OriginURL, ch.New[i].OriginURL)
			}
		}
	}
	if !o.apply {
		fmt.Printf("\ndry run: %d domain(s); add -apply to send them\n", len(changes))
		return nil
	}

	if o.restore == "" {
		name := fmt.Sprintf("origins-backup-%s.json", time.Now().Format("20060102-150405"))
		if err := writeBackup(name, changes); err != nil {
			return err
		}
		fmt.Println("\nbackup written to", name)
	}

	failed := 0
	for _, ch := range changes {
		if err := c.replaceRoutes(ch.ID, ch.New); err != nil {
			failed++
			fmt.Printf("FAIL %s: %v\n", ch.Hostname, err)
			continue
		}
		fmt.Printf("ok   %s\n", ch.Hostname)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d update(s) failed", failed, len(changes))
	}
	return nil
}

// planChanges returns the domains with at least one route whose origin host
// is in from, with those hosts swapped for to. Scheme, port, path, query and
// the other route fields are kept.
func planChanges(domains []domain, from map[string]bool, to string, only, exclude map[string]bool) ([]change, error) {
	var changes []change
	for _, d := range domains {
		name := strings.ToLower(d.Hostname)
		if (len(only) > 0 && !only[name]) || exclude[name] {
			continue
		}
		old := d.Routes
		if len(old) == 0 {
			old = []route{{Path: "/", OriginURL: d.OriginURL}}
		}
		updated := make([]route, len(old))
		changed := false
		for i, r := range old {
			newURL, ok, err := rewriteHost(r.OriginURL, from, to)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", d.Hostname, r.Path, err)
			}
			updated[i] = r
			updated[i].OriginURL = newURL
			changed = changed || ok
		}
		if changed {
			changes = append(changes, change{ID: d.ID, Hostname: d.Hostname, Old: old, New: updated})
		}
	}
	return changes, nil
}

func rewriteHost(raw string, from map[string]bool, to string) (string, bool, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("parse origin %q: %w", raw, err)
	}
	if !from[strings.ToLower(u.Hostname())] {
		return raw, false, nil
	}
	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(to, port)
	} else if strings.Contains(to, ":") {
		u.Host = "[" + to + "]"
	} else {
		u.Host = to
	}
	return u.String(), true, nil
}

func splitSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			set[part] = true
		}
	}
	return set
}

func writeBackup(name string, changes []change) error {
	data, err := json.MarshalIndent(changes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o600)
}

func readBackup(name string) ([]change, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var changes []change
	if err := json.Unmarshal(data, &changes); err != nil {
		return nil, fmt.Errorf("read backup %s: %w", name, err)
	}
	for _, ch := range changes {
		if len(ch.Old) == 0 || len(ch.Old) != len(ch.New) {
			return nil, fmt.Errorf("read backup %s: %s has no usable routes (older backup format?)", name, ch.Hostname)
		}
	}
	return changes, nil
}

type client struct {
	base  string
	token string
	http  *http.Client
}

func (c *client) login(user, password string) error {
	var res struct {
		Token string `json:"token"`
	}
	body := map[string]string{"username": user, "password": password}
	if err := c.do(http.MethodPost, "/api/auth/login", body, &res); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	c.token = res.Token
	return nil
}

func (c *client) listDomains() ([]domain, error) {
	var all []domain
	cursor := ""
	for {
		q := url.Values{"pageSize": {"100"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var res struct {
			Items      []domain `json:"items"`
			NextCursor string   `json:"nextCursor"`
		}
		if err := c.do(http.MethodGet, "/api/domains?"+q.Encode(), nil, &res); err != nil {
			return nil, fmt.Errorf("list domains: %w", err)
		}
		all = append(all, res.Items...)
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
}

func (c *client) replaceRoutes(id string, routes []route) error {
	return c.do(http.MethodPut, "/api/domains/"+url.PathEscape(id)+"/routes", map[string]any{"routes": routes}, nil)
}

func (c *client) do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
