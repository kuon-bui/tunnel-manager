// Command rewrite-origins rewrites the host part of every managed domain's
// origin URL through the tunnel-manager HTTP API.
//
// Typical use when moving the backend to another machine while origins stay
// on the old one: replace localhost origins with the old machine's LAN IP.
//
//	TM_PASSWORD=... go run ./cmd/rewrite-origins -api http://pi.local:8080 -to 192.168.1.50
//	TM_PASSWORD=... go run ./cmd/rewrite-origins -api http://pi.local:8080 -to 192.168.1.50 -apply
//	TM_PASSWORD=... go run ./cmd/rewrite-origins -api http://pi.local:8080 -restore origins-backup-....json
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

type domain struct {
	ID        string `json:"id"`
	Hostname  string `json:"hostname"`
	OriginURL string `json:"originUrl"`
}

type change struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Old      string `json:"old"`
	New      string `json:"new"`
}

func main() {
	api := flag.String("api", "http://localhost:8080", "backend base URL")
	user := flag.String("user", "admin", "admin username (password from TM_PASSWORD)")
	from := flag.String("from", "localhost,127.0.0.1,::1,0.0.0.0", "comma-separated origin hosts to replace")
	to := flag.String("to", "", "new origin host, e.g. the old machine's LAN IP")
	only := flag.String("only", "", "comma-separated public hostnames to limit the change to")
	apply := flag.Bool("apply", false, "send the updates (default: dry run)")
	restore := flag.String("restore", "", "backup file to restore old origin URLs from")
	flag.Parse()

	if err := run(*api, *user, *from, *to, *only, *apply, *restore); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(api, user, from, to, only string, apply bool, restore string) error {
	if restore == "" && to == "" {
		return errors.New("-to is required (or use -restore)")
	}
	c := &client{base: strings.TrimRight(api, "/"), http: &http.Client{Timeout: 30 * time.Second}}
	c.token = os.Getenv("TM_TOKEN")
	if c.token == "" {
		password := os.Getenv("TM_PASSWORD")
		if password == "" {
			return errors.New("set TM_PASSWORD (or TM_TOKEN)")
		}
		if err := c.login(user, password); err != nil {
			return err
		}
	}

	var changes []change
	if restore != "" {
		backup, err := readBackup(restore)
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
		changes, err = planChanges(domains, splitSet(from), to, splitSet(only))
		if err != nil {
			return err
		}
	}

	if len(changes) == 0 {
		fmt.Println("nothing to change")
		return nil
	}
	for _, ch := range changes {
		fmt.Printf("%-40s %s -> %s\n", ch.Hostname, ch.Old, ch.New)
	}
	if !apply {
		fmt.Printf("\ndry run: %d change(s); add -apply to send them\n", len(changes))
		return nil
	}

	if restore == "" {
		name := fmt.Sprintf("origins-backup-%s.json", time.Now().Format("20060102-150405"))
		if err := writeBackup(name, changes); err != nil {
			return err
		}
		fmt.Println("\nbackup written to", name)
	}

	failed := 0
	for _, ch := range changes {
		if err := c.updateOrigin(ch.ID, ch.New); err != nil {
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

// planChanges lists the domains whose origin host is in from, with the host
// swapped for to. Scheme, port, path and query are kept.
func planChanges(domains []domain, from map[string]bool, to string, only map[string]bool) ([]change, error) {
	var changes []change
	for _, d := range domains {
		if len(only) > 0 && !only[strings.ToLower(d.Hostname)] {
			continue
		}
		newURL, ok, err := rewriteHost(d.OriginURL, from, to)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Hostname, err)
		}
		if ok {
			changes = append(changes, change{ID: d.ID, Hostname: d.Hostname, Old: d.OriginURL, New: newURL})
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

func (c *client) updateOrigin(id, originURL string) error {
	return c.do(http.MethodPut, "/api/domains/"+url.PathEscape(id), map[string]string{"originUrl": originURL}, nil)
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
