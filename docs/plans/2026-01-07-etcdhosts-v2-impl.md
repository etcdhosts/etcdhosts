# etcdhosts v2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete rewrite of etcdhosts CoreDNS plugin with modern Go, advanced DNS features, and comprehensive testing.

**Architecture:** Modular design with internal packages for hosts parsing, etcd storage, health checking, and load balancing. CoreDNS plugin layer delegates to internal components.

**Tech Stack:** Go 1.25+, CoreDNS v1.13.2, etcd client v3.6.7, embedded etcd v3.6.7 for testing, testcontainers-go for health check tests.

---

## Task 1: Project Scaffold

**Files:**
- Delete: All existing `.go` files in root
- Create: `go.mod`
- Create: `internal/hosts/record.go`
- Create: `Taskfile.yaml`

**Step 1: Remove old source files**

```bash
rm -f *.go
rm -f go.mod go.sum
```

**Step 2: Initialize go.mod**

Create `go.mod`:
```go
module github.com/etcdhosts/etcdhosts

go 1.25
```

**Step 3: Create directory structure**

```bash
mkdir -p internal/hosts internal/etcd internal/healthcheck internal/loadbalance
mkdir -p testdata
```

**Step 4: Create record.go with basic types**

Create `internal/hosts/record.go`:
```go
package hosts

import (
	"net"
	"time"
)

// Record represents a single hosts entry with extended attributes.
type Record struct {
	Hostname string   // Hostname (supports wildcards like *.example.com)
	IP       net.IP   // IP address
	TTL      uint32   // DNS TTL in seconds, 0 means use default
	Weight   int      // Weight for load balancing, default 1
	Health   *Health  // Health check config, nil means disabled
}

// Health defines health check configuration.
type Health struct {
	Type     CheckType     // tcp / http / https / icmp
	Port     int           // Target port (not used for icmp)
	Path     string        // URL path for http/https
	Interval time.Duration // Check interval, default 10s
	Timeout  time.Duration // Check timeout, default 3s
}

// CheckType represents the health check protocol.
type CheckType string

const (
	CheckTCP   CheckType = "tcp"
	CheckHTTP  CheckType = "http"
	CheckHTTPS CheckType = "https"
	CheckICMP  CheckType = "icmp"
)

// IPStatus tracks runtime health status of an IP.
type IPStatus struct {
	IP        net.IP
	Healthy   bool
	LastCheck time.Time
	Failures  int // Consecutive failure count
}
```

**Step 5: Update Taskfile.yaml**

Create `Taskfile.yaml`:
```yaml
version: '3'

vars:
  COREDNS_VERSION: 'v1.13.2'

tasks:
  clean:
    desc: Clean build artifacts
    cmds:
      - rm -rf coredns dist

  test:
    desc: Run all tests
    cmds:
      - go test -v -race ./...

  test-cover:
    desc: Run tests with coverage
    cmds:
      - go test -v -race -coverprofile=coverage.out ./...
      - go tool cover -html=coverage.out -o coverage.html

  lint:
    desc: Run linter
    cmds:
      - golangci-lint run ./...

  clone-source:
    desc: Clone CoreDNS source
    cmds:
      - task: clean
      - mkdir -p dist
      - git clone --depth 1 --branch {{.COREDNS_VERSION}} https://github.com/coredns/coredns.git coredns

  add-plugin:
    desc: Add etcdhosts plugin to CoreDNS
    dir: coredns
    deps: [clone-source]
    vars:
      ROOT_DIR:
        sh: git rev-parse --show-toplevel
    cmds:
      - mkdir -p plugin/etcdhosts
      - cp ../plugin.go ../setup.go ../handler.go ../metrics.go plugin/etcdhosts/
      - cp -r ../internal plugin/etcdhosts/
      - |
        {{if eq OS "darwin"}}
        gsed -i '/^hosts:hosts/i\etcdhosts:etcdhosts' plugin.cfg
        {{else}}
        sed -i '/^hosts:hosts/i\etcdhosts:etcdhosts' plugin.cfg
        {{end}}

  build:
    desc: Build CoreDNS with etcdhosts
    dir: coredns
    cmds:
      - make -f Makefile gen
      - make -f Makefile.release build tar DOCKER=coredns
      - mv release/* ../dist

  all:
    desc: Full build pipeline
    cmds:
      - task: test
      - task: clone-source
      - task: add-plugin
      - task: build
```

**Step 6: Run go mod tidy**

```bash
go mod tidy
```

**Step 7: Verify structure**

```bash
find . -type f -name "*.go" | head -20
```

Expected: Shows `internal/hosts/record.go`

**Step 8: Commit**

```bash
git add -A
git commit -m "chore: scaffold project structure for v2 rewrite"
```

---

## Task 2: Hosts Parser - Basic Format

**Files:**
- Create: `internal/hosts/parser.go`
- Create: `internal/hosts/parser_test.go`

**Step 1: Write failing test for basic parsing**

Create `internal/hosts/parser_test.go`:
```go
package hosts

import (
	"net"
	"testing"
)

func TestParser_BasicFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Record
		wantErr bool
	}{
		{
			name:  "single ipv4 record",
			input: "192.168.1.1 example.com",
			want: []Record{
				{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
			},
		},
		{
			name:  "single ipv6 record",
			input: "2001:db8::1 ipv6.example.com",
			want: []Record{
				{Hostname: "ipv6.example.com.", IP: net.ParseIP("2001:db8::1"), Weight: 1},
			},
		},
		{
			name:  "multiple hostnames per line",
			input: "192.168.1.1 host1.example.com host2.example.com",
			want: []Record{
				{Hostname: "host1.example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
				{Hostname: "host2.example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
			},
		},
		{
			name:  "comment line ignored",
			input: "# this is a comment\n192.168.1.1 example.com",
			want: []Record{
				{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
			},
		},
		{
			name:  "empty lines ignored",
			input: "\n\n192.168.1.1 example.com\n\n",
			want: []Record{
				{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
			},
		},
		{
			name:    "invalid ip skipped",
			input:   "invalid example.com",
			want:    []Record{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			got, err := p.Parse([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("Parse() got %d records, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i].Hostname != tt.want[i].Hostname {
					t.Errorf("record[%d].Hostname = %v, want %v", i, got[i].Hostname, tt.want[i].Hostname)
				}
				if !got[i].IP.Equal(tt.want[i].IP) {
					t.Errorf("record[%d].IP = %v, want %v", i, got[i].IP, tt.want[i].IP)
				}
				if got[i].Weight != tt.want[i].Weight {
					t.Errorf("record[%d].Weight = %v, want %v", i, got[i].Weight, tt.want[i].Weight)
				}
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test -v ./internal/hosts/... -run TestParser_BasicFormat
```

Expected: FAIL with "undefined: NewParser"

**Step 3: Implement basic parser**

Create `internal/hosts/parser.go`:
```go
package hosts

import (
	"bufio"
	"bytes"
	"net"
	"strings"
)

// Parser parses hosts file format with extended etcdhosts syntax.
type Parser struct{}

// NewParser creates a new Parser instance.
func NewParser() *Parser {
	return &Parser{}
}

// Parse parses hosts data and returns a slice of Records.
func (p *Parser) Parse(data []byte) ([]Record, error) {
	var records []Record
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Text()
		rec := p.parseLine(line)
		records = append(records, rec...)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

// parseLine parses a single line and returns records.
func (p *Parser) parseLine(line string) []Record {
	// Remove leading/trailing whitespace
	line = strings.TrimSpace(line)

	// Skip empty lines
	if line == "" {
		return nil
	}

	// Skip pure comment lines
	if strings.HasPrefix(line, "#") {
		return nil
	}

	// Split line into main part and comment
	mainPart := line
	if idx := strings.Index(line, "#"); idx >= 0 {
		mainPart = strings.TrimSpace(line[:idx])
	}

	// Parse fields
	fields := strings.Fields(mainPart)
	if len(fields) < 2 {
		return nil
	}

	// Parse IP
	ip := parseIP(fields[0])
	if ip == nil {
		return nil
	}

	// Parse hostnames
	var records []Record
	for i := 1; i < len(fields); i++ {
		hostname := normalizeHostname(fields[i])
		records = append(records, Record{
			Hostname: hostname,
			IP:       ip,
			Weight:   1, // Default weight
		})
	}

	return records
}

// parseIP parses an IP address, discarding any IPv6 zone info.
func parseIP(addr string) net.IP {
	if i := strings.Index(addr, "%"); i >= 0 {
		addr = addr[:i]
	}
	return net.ParseIP(addr)
}

// normalizeHostname ensures hostname ends with a dot (FQDN).
func normalizeHostname(host string) string {
	host = strings.ToLower(host)
	if !strings.HasSuffix(host, ".") {
		host += "."
	}
	return host
}
```

**Step 4: Run test to verify it passes**

```bash
go test -v ./internal/hosts/... -run TestParser_BasicFormat
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat(parser): implement basic hosts format parsing"
```

---

## Task 3: Hosts Parser - Extended Format

**Files:**
- Modify: `internal/hosts/parser.go`
- Modify: `internal/hosts/parser_test.go`

**Step 1: Write failing test for extended format**

Add to `internal/hosts/parser_test.go`:
```go
func TestParser_ExtendedFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Record
		wantErr bool
	}{
		{
			name:  "with weight",
			input: "192.168.1.1 example.com # +etcdhosts weight=10",
			want:  Record{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 10},
		},
		{
			name:  "with ttl",
			input: "192.168.1.1 example.com # +etcdhosts ttl=60",
			want:  Record{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1, TTL: 60},
		},
		{
			name:  "with weight and ttl",
			input: "192.168.1.1 example.com # +etcdhosts weight=5 ttl=300",
			want:  Record{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 5, TTL: 300},
		},
		{
			name:  "with tcp health check",
			input: "192.168.1.1 example.com # +etcdhosts hc=tcp:3306",
			want: Record{
				Hostname: "example.com.",
				IP:       net.ParseIP("192.168.1.1"),
				Weight:   1,
				Health:   &Health{Type: CheckTCP, Port: 3306},
			},
		},
		{
			name:  "with http health check",
			input: "192.168.1.1 example.com # +etcdhosts hc=http:8080/health",
			want: Record{
				Hostname: "example.com.",
				IP:       net.ParseIP("192.168.1.1"),
				Weight:   1,
				Health:   &Health{Type: CheckHTTP, Port: 8080, Path: "/health"},
			},
		},
		{
			name:  "with https health check",
			input: "192.168.1.1 example.com # +etcdhosts hc=https:443/ping",
			want: Record{
				Hostname: "example.com.",
				IP:       net.ParseIP("192.168.1.1"),
				Weight:   1,
				Health:   &Health{Type: CheckHTTPS, Port: 443, Path: "/ping"},
			},
		},
		{
			name:  "with icmp health check",
			input: "192.168.1.1 example.com # +etcdhosts hc=icmp",
			want: Record{
				Hostname: "example.com.",
				IP:       net.ParseIP("192.168.1.1"),
				Weight:   1,
				Health:   &Health{Type: CheckICMP},
			},
		},
		{
			name:  "full example",
			input: "10.0.0.1 api.example.com # +etcdhosts weight=3 ttl=60 hc=http:80/status",
			want: Record{
				Hostname: "api.example.com.",
				IP:       net.ParseIP("10.0.0.1"),
				Weight:   3,
				TTL:      60,
				Health:   &Health{Type: CheckHTTP, Port: 80, Path: "/status"},
			},
		},
		{
			name:  "regular comment ignored",
			input: "192.168.1.1 example.com # regular comment without marker",
			want:  Record{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			got, err := p.Parse([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != 1 {
				t.Errorf("Parse() got %d records, want 1", len(got))
				return
			}
			r := got[0]
			if r.Hostname != tt.want.Hostname {
				t.Errorf("Hostname = %v, want %v", r.Hostname, tt.want.Hostname)
			}
			if !r.IP.Equal(tt.want.IP) {
				t.Errorf("IP = %v, want %v", r.IP, tt.want.IP)
			}
			if r.Weight != tt.want.Weight {
				t.Errorf("Weight = %v, want %v", r.Weight, tt.want.Weight)
			}
			if r.TTL != tt.want.TTL {
				t.Errorf("TTL = %v, want %v", r.TTL, tt.want.TTL)
			}
			if tt.want.Health != nil {
				if r.Health == nil {
					t.Errorf("Health = nil, want %+v", tt.want.Health)
				} else {
					if r.Health.Type != tt.want.Health.Type {
						t.Errorf("Health.Type = %v, want %v", r.Health.Type, tt.want.Health.Type)
					}
					if r.Health.Port != tt.want.Health.Port {
						t.Errorf("Health.Port = %v, want %v", r.Health.Port, tt.want.Health.Port)
					}
					if r.Health.Path != tt.want.Health.Path {
						t.Errorf("Health.Path = %v, want %v", r.Health.Path, tt.want.Health.Path)
					}
				}
			} else if r.Health != nil {
				t.Errorf("Health = %+v, want nil", r.Health)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test -v ./internal/hosts/... -run TestParser_ExtendedFormat
```

Expected: FAIL (extended attributes not parsed)

**Step 3: Implement extended format parsing**

Update `internal/hosts/parser.go`, replace `parseLine` and add helper functions:
```go
// parseLine parses a single line and returns records.
func (p *Parser) parseLine(line string) []Record {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	// Split line into main part and comment
	var mainPart, comment string
	if idx := strings.Index(line, "#"); idx >= 0 {
		mainPart = strings.TrimSpace(line[:idx])
		comment = strings.TrimSpace(line[idx+1:])
	} else {
		mainPart = line
	}

	// Parse fields from main part
	fields := strings.Fields(mainPart)
	if len(fields) < 2 {
		return nil
	}

	ip := parseIP(fields[0])
	if ip == nil {
		return nil
	}

	// Parse extended attributes from comment
	ext := p.parseExtended(comment)

	// Build records for each hostname
	var records []Record
	for i := 1; i < len(fields); i++ {
		hostname := normalizeHostname(fields[i])
		records = append(records, Record{
			Hostname: hostname,
			IP:       ip,
			Weight:   ext.weight,
			TTL:      ext.ttl,
			Health:   ext.health,
		})
	}

	return records
}

// extendedAttrs holds parsed extended attributes.
type extendedAttrs struct {
	weight int
	ttl    uint32
	health *Health
}

// parseExtended parses the comment for +etcdhosts attributes.
func (p *Parser) parseExtended(comment string) extendedAttrs {
	ext := extendedAttrs{weight: 1} // Default weight

	// Look for +etcdhosts marker
	const marker = "+etcdhosts"
	idx := strings.Index(comment, marker)
	if idx < 0 {
		return ext
	}

	// Parse attributes after marker
	attrPart := strings.TrimSpace(comment[idx+len(marker):])
	attrs := strings.Fields(attrPart)

	for _, attr := range attrs {
		key, value, ok := strings.Cut(attr, "=")
		if !ok {
			continue
		}
		switch key {
		case "weight":
			if w, err := parseInt(value); err == nil && w > 0 {
				ext.weight = w
			}
		case "ttl":
			if t, err := parseInt(value); err == nil && t > 0 {
				ext.ttl = uint32(t)
			}
		case "hc":
			ext.health = p.parseHealthCheck(value)
		}
	}

	return ext
}

// parseHealthCheck parses health check spec like "tcp:3306" or "http:8080/health".
func (p *Parser) parseHealthCheck(spec string) *Health {
	if spec == "" {
		return nil
	}

	// Handle icmp (no port)
	if spec == "icmp" {
		return &Health{Type: CheckICMP}
	}

	// Parse type:port or type:port/path
	typePart, rest, ok := strings.Cut(spec, ":")
	if !ok {
		return nil
	}

	checkType := CheckType(typePart)
	switch checkType {
	case CheckTCP, CheckHTTP, CheckHTTPS:
		// Valid types
	default:
		return nil
	}

	// Parse port and optional path
	var portStr, path string
	if idx := strings.Index(rest, "/"); idx >= 0 {
		portStr = rest[:idx]
		path = rest[idx:]
	} else {
		portStr = rest
	}

	port, err := parseInt(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil
	}

	return &Health{
		Type: checkType,
		Port: port,
		Path: path,
	}
}

// parseInt parses an integer from string.
func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, &parseError{s}
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

type parseError struct {
	s string
}

func (e *parseError) Error() string {
	return "invalid number: " + e.s
}
```

**Step 4: Run test to verify it passes**

```bash
go test -v ./internal/hosts/... -run TestParser_ExtendedFormat
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat(parser): implement extended etcdhosts format parsing"
```

---

## Task 4: Hosts Store - Basic Lookup

**Files:**
- Create: `internal/hosts/store.go`
- Create: `internal/hosts/store_test.go`

**Step 1: Write failing test for basic store**

Create `internal/hosts/store_test.go`:
```go
package hosts

import (
	"net"
	"testing"
)

func TestStore_BasicLookup(t *testing.T) {
	store := NewStore()

	records := []Record{
		{Hostname: "example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
		{Hostname: "example.com.", IP: net.ParseIP("192.168.1.2"), Weight: 1},
		{Hostname: "ipv6.example.com.", IP: net.ParseIP("2001:db8::1"), Weight: 1},
	}

	store.Update(records)

	t.Run("lookup ipv4", func(t *testing.T) {
		ips := store.LookupV4("example.com.")
		if len(ips) != 2 {
			t.Errorf("got %d IPs, want 2", len(ips))
		}
	})

	t.Run("lookup ipv6", func(t *testing.T) {
		ips := store.LookupV6("ipv6.example.com.")
		if len(ips) != 1 {
			t.Errorf("got %d IPs, want 1", len(ips))
		}
	})

	t.Run("lookup not found", func(t *testing.T) {
		ips := store.LookupV4("notfound.example.com.")
		if len(ips) != 0 {
			t.Errorf("got %d IPs, want 0", len(ips))
		}
	})

	t.Run("reverse lookup", func(t *testing.T) {
		hosts := store.LookupAddr("192.168.1.1")
		if len(hosts) != 1 || hosts[0] != "example.com." {
			t.Errorf("got %v, want [example.com.]", hosts)
		}
	})
}

func TestStore_CaseInsensitive(t *testing.T) {
	store := NewStore()
	store.Update([]Record{
		{Hostname: "Example.COM.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
	})

	ips := store.LookupV4("example.com.")
	if len(ips) != 1 {
		t.Errorf("case insensitive lookup failed, got %d IPs", len(ips))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test -v ./internal/hosts/... -run TestStore
```

Expected: FAIL with "undefined: NewStore"

**Step 3: Implement basic store**

Create `internal/hosts/store.go`:
```go
package hosts

import (
	"net"
	"strings"
	"sync"
)

// Store holds parsed hosts records with fast lookup maps.
type Store struct {
	mu      sync.RWMutex
	name4   map[string][]Entry // hostname -> IPv4 entries
	name6   map[string][]Entry // hostname -> IPv6 entries
	addr    map[string][]string // IP -> hostnames (reverse lookup)
	records []Record            // all records for iteration
}

// Entry represents a single IP with its metadata.
type Entry struct {
	IP     net.IP
	TTL    uint32
	Weight int
	Health *Health
}

// NewStore creates a new empty Store.
func NewStore() *Store {
	return &Store{
		name4: make(map[string][]Entry),
		name6: make(map[string][]Entry),
		addr:  make(map[string][]string),
	}
}

// Update replaces all records in the store.
func (s *Store) Update(records []Record) {
	name4 := make(map[string][]Entry)
	name6 := make(map[string][]Entry)
	addr := make(map[string][]string)

	for _, r := range records {
		hostname := strings.ToLower(r.Hostname)
		entry := Entry{
			IP:     r.IP,
			TTL:    r.TTL,
			Weight: r.Weight,
			Health: r.Health,
		}

		if r.IP.To4() != nil {
			name4[hostname] = append(name4[hostname], entry)
		} else {
			name6[hostname] = append(name6[hostname], entry)
		}

		// Reverse mapping
		ipStr := r.IP.String()
		addr[ipStr] = appendUnique(addr[ipStr], hostname)
	}

	s.mu.Lock()
	s.name4 = name4
	s.name6 = name6
	s.addr = addr
	s.records = records
	s.mu.Unlock()
}

// LookupV4 returns IPv4 addresses for hostname.
func (s *Store) LookupV4(hostname string) []net.IP {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := s.name4[strings.ToLower(hostname)]
	ips := make([]net.IP, len(entries))
	for i, e := range entries {
		ips[i] = e.IP
	}
	return ips
}

// LookupV6 returns IPv6 addresses for hostname.
func (s *Store) LookupV6(hostname string) []net.IP {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := s.name6[strings.ToLower(hostname)]
	ips := make([]net.IP, len(entries))
	for i, e := range entries {
		ips[i] = e.IP
	}
	return ips
}

// LookupAddr returns hostnames for an IP address (reverse lookup).
func (s *Store) LookupAddr(addr string) []string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	hosts := s.addr[ip.String()]
	if len(hosts) == 0 {
		return nil
	}

	result := make([]string, len(hosts))
	copy(result, hosts)
	return result
}

// Len returns total number of records.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// appendUnique appends s to slice if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}
```

**Step 4: Run test to verify it passes**

```bash
go test -v ./internal/hosts/... -run TestStore
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat(store): implement basic hosts store with lookup"
```

---

## Task 5: Wildcard Matching

**Files:**
- Create: `internal/hosts/wildcard.go`
- Create: `internal/hosts/wildcard_test.go`
- Modify: `internal/hosts/store.go`

**Step 1: Write failing test for wildcard matching**

Create `internal/hosts/wildcard_test.go`:
```go
package hosts

import (
	"testing"
)

func TestWildcard_Match(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*.example.com.", "foo.example.com.", true},
		{"*.example.com.", "bar.example.com.", true},
		{"*.example.com.", "example.com.", false},
		{"*.example.com.", "foo.bar.example.com.", false},
		{"*.sub.example.com.", "foo.sub.example.com.", true},
		{"*.sub.example.com.", "foo.example.com.", false},
		{"example.com.", "example.com.", true},
		{"example.com.", "foo.example.com.", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"->"+tt.name, func(t *testing.T) {
			got := wildcardMatch(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("wildcardMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestWildcard_Priority(t *testing.T) {
	// Priority: exact > longest wildcard > shorter wildcard
	tests := []struct {
		patterns []string
		name     string
		want     string
	}{
		{
			patterns: []string{"foo.example.com.", "*.example.com."},
			name:     "foo.example.com.",
			want:     "foo.example.com.",
		},
		{
			patterns: []string{"*.sub.example.com.", "*.example.com."},
			name:     "foo.sub.example.com.",
			want:     "*.sub.example.com.",
		},
		{
			patterns: []string{"*.example.com."},
			name:     "foo.example.com.",
			want:     "*.example.com.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectBestMatch(tt.patterns, tt.name)
			if got != tt.want {
				t.Errorf("selectBestMatch(%v, %q) = %q, want %q", tt.patterns, tt.name, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test -v ./internal/hosts/... -run TestWildcard
```

Expected: FAIL with "undefined: wildcardMatch"

**Step 3: Implement wildcard matching**

Create `internal/hosts/wildcard.go`:
```go
package hosts

import (
	"strings"
)

// wildcardMatch checks if name matches the wildcard pattern.
// Pattern "*.example.com." matches "foo.example.com." but not "foo.bar.example.com."
func wildcardMatch(pattern, name string) bool {
	pattern = strings.ToLower(pattern)
	name = strings.ToLower(name)

	// Exact match
	if pattern == name {
		return true
	}

	// Check for wildcard
	if !strings.HasPrefix(pattern, "*.") {
		return false
	}

	// Get the suffix after *
	suffix := pattern[1:] // ".example.com."

	// Name must end with suffix
	if !strings.HasSuffix(name, suffix) {
		return false
	}

	// Name prefix (before suffix) must not contain dots
	// e.g., "foo.example.com." - ".example.com." = "foo"
	prefix := name[:len(name)-len(suffix)]
	return !strings.Contains(prefix, ".")
}

// selectBestMatch selects the best matching pattern for a hostname.
// Priority: exact match > longest wildcard > shorter wildcard
func selectBestMatch(patterns []string, name string) string {
	name = strings.ToLower(name)
	var bestMatch string
	bestScore := -1

	for _, pattern := range patterns {
		pattern = strings.ToLower(pattern)
		if !wildcardMatch(pattern, name) {
			continue
		}

		score := matchScore(pattern, name)
		if score > bestScore {
			bestScore = score
			bestMatch = pattern
		}
	}

	return bestMatch
}

// matchScore returns a score for pattern match quality.
// Higher is better. Exact match = MaxInt, wildcard = length of pattern.
func matchScore(pattern, name string) int {
	if pattern == name {
		return 1<<30 // Exact match gets highest score
	}
	// Wildcard: longer pattern = more specific = higher score
	return len(pattern)
}

// isWildcard checks if pattern is a wildcard pattern.
func isWildcard(pattern string) bool {
	return strings.HasPrefix(pattern, "*.")
}
```

**Step 4: Run test to verify it passes**

```bash
go test -v ./internal/hosts/... -run TestWildcard
```

Expected: PASS

**Step 5: Update store with wildcard support**

Add to `internal/hosts/store.go`:
```go
// LookupV4WithWildcard returns IPv4 entries for hostname, including wildcard matches.
func (s *Store) LookupV4WithWildcard(hostname string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lookupWithWildcard(s.name4, hostname)
}

// LookupV6WithWildcard returns IPv6 entries for hostname, including wildcard matches.
func (s *Store) LookupV6WithWildcard(hostname string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lookupWithWildcard(s.name6, hostname)
}

func (s *Store) lookupWithWildcard(m map[string][]Entry, hostname string) []Entry {
	hostname = strings.ToLower(hostname)

	// Try exact match first
	if entries, ok := m[hostname]; ok {
		result := make([]Entry, len(entries))
		copy(result, entries)
		return result
	}

	// Collect all matching wildcard patterns
	var patterns []string
	for pattern := range m {
		if isWildcard(pattern) && wildcardMatch(pattern, hostname) {
			patterns = append(patterns, pattern)
		}
	}

	if len(patterns) == 0 {
		return nil
	}

	// Select best match
	best := selectBestMatch(patterns, hostname)
	if best == "" {
		return nil
	}

	entries := m[best]
	result := make([]Entry, len(entries))
	copy(result, entries)
	return result
}
```

**Step 6: Add test for store wildcard lookup**

Add to `internal/hosts/store_test.go`:
```go
func TestStore_WildcardLookup(t *testing.T) {
	store := NewStore()
	store.Update([]Record{
		{Hostname: "*.example.com.", IP: net.ParseIP("192.168.1.1"), Weight: 1},
		{Hostname: "*.sub.example.com.", IP: net.ParseIP("192.168.1.2"), Weight: 1},
		{Hostname: "exact.example.com.", IP: net.ParseIP("192.168.1.3"), Weight: 1},
	})

	t.Run("exact match priority", func(t *testing.T) {
		entries := store.LookupV4WithWildcard("exact.example.com.")
		if len(entries) != 1 || !entries[0].IP.Equal(net.ParseIP("192.168.1.3")) {
			t.Errorf("expected exact match IP 192.168.1.3, got %v", entries)
		}
	})

	t.Run("wildcard match", func(t *testing.T) {
		entries := store.LookupV4WithWildcard("foo.example.com.")
		if len(entries) != 1 || !entries[0].IP.Equal(net.ParseIP("192.168.1.1")) {
			t.Errorf("expected wildcard match IP 192.168.1.1, got %v", entries)
		}
	})

	t.Run("longer wildcard priority", func(t *testing.T) {
		entries := store.LookupV4WithWildcard("foo.sub.example.com.")
		if len(entries) != 1 || !entries[0].IP.Equal(net.ParseIP("192.168.1.2")) {
			t.Errorf("expected longer wildcard IP 192.168.1.2, got %v", entries)
		}
	})

	t.Run("no match", func(t *testing.T) {
		entries := store.LookupV4WithWildcard("foo.other.com.")
		if len(entries) != 0 {
			t.Errorf("expected no match, got %v", entries)
		}
	})
}
```

**Step 7: Run all tests**

```bash
go test -v ./internal/hosts/...
```

Expected: All PASS

**Step 8: Commit**

```bash
git add -A
git commit -m "feat(store): implement wildcard hostname matching"
```

---

## Task 6: Weighted Load Balancer

**Files:**
- Create: `internal/loadbalance/weighted.go`
- Create: `internal/loadbalance/weighted_test.go`

**Step 1: Write failing test**

Create `internal/loadbalance/weighted_test.go`:
```go
package loadbalance

import (
	"net"
	"testing"
)

func TestWeightedBalancer_SingleEntry(t *testing.T) {
	b := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 10, Healthy: true},
	}

	result := b.Select(entries)
	if len(result) != 1 {
		t.Errorf("got %d IPs, want 1", len(result))
	}
	if !result[0].Equal(net.ParseIP("192.168.1.1")) {
		t.Errorf("got %v, want 192.168.1.1", result[0])
	}
}

func TestWeightedBalancer_FilterUnhealthy(t *testing.T) {
	b := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 1, Healthy: true},
		{IP: net.ParseIP("192.168.1.2"), Weight: 1, Healthy: false},
		{IP: net.ParseIP("192.168.1.3"), Weight: 1, Healthy: true},
	}

	result := b.Select(entries)
	if len(result) != 2 {
		t.Errorf("got %d IPs, want 2 (unhealthy filtered)", len(result))
	}

	// Check unhealthy IP is not in result
	for _, ip := range result {
		if ip.Equal(net.ParseIP("192.168.1.2")) {
			t.Error("unhealthy IP should be filtered")
		}
	}
}

func TestWeightedBalancer_WeightDistribution(t *testing.T) {
	b := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 3, Healthy: true},
		{IP: net.ParseIP("192.168.1.2"), Weight: 1, Healthy: true},
	}

	// Run multiple times and count first position
	counts := make(map[string]int)
	iterations := 10000
	for i := 0; i < iterations; i++ {
		result := b.Select(entries)
		if len(result) > 0 {
			counts[result[0].String()]++
		}
	}

	// With weight 3:1, expect roughly 75%:25% distribution
	ip1Count := counts["192.168.1.1"]
	ratio := float64(ip1Count) / float64(iterations)

	// Allow 5% tolerance
	if ratio < 0.70 || ratio > 0.80 {
		t.Errorf("weight distribution off: IP1 got %.2f%%, expected ~75%%", ratio*100)
	}
}

func TestWeightedBalancer_AllUnhealthy(t *testing.T) {
	b := NewWeightedBalancer()
	entries := []Entry{
		{IP: net.ParseIP("192.168.1.1"), Weight: 1, Healthy: false},
		{IP: net.ParseIP("192.168.1.2"), Weight: 1, Healthy: false},
	}

	result := b.Select(entries)
	if len(result) != 0 {
		t.Errorf("got %d IPs, want 0 when all unhealthy", len(result))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test -v ./internal/loadbalance/... -run TestWeightedBalancer
```

Expected: FAIL with "undefined: NewWeightedBalancer"

**Step 3: Implement weighted balancer**

Create `internal/loadbalance/weighted.go`:
```go
package loadbalance

import (
	"math/rand/v2"
	"net"
)

// Entry represents an IP entry for load balancing.
type Entry struct {
	IP      net.IP
	Weight  int
	Healthy bool
}

// WeightedBalancer implements weighted random load balancing.
type WeightedBalancer struct{}

// NewWeightedBalancer creates a new WeightedBalancer.
func NewWeightedBalancer() *WeightedBalancer {
	return &WeightedBalancer{}
}

// Select returns IPs sorted by weighted random selection.
// Unhealthy entries are filtered out.
// Single entry is returned directly without weight calculation.
func (b *WeightedBalancer) Select(entries []Entry) []net.IP {
	// Filter healthy entries
	healthy := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Healthy {
			healthy = append(healthy, e)
		}
	}

	if len(healthy) == 0 {
		return nil
	}

	// Single entry: return directly
	if len(healthy) == 1 {
		return []net.IP{healthy[0].IP}
	}

	// Weighted random shuffle
	return b.weightedShuffle(healthy)
}

// weightedShuffle returns IPs in weighted random order.
func (b *WeightedBalancer) weightedShuffle(entries []Entry) []net.IP {
	n := len(entries)
	result := make([]net.IP, n)

	// Create working copy with remaining weights
	type item struct {
		ip     net.IP
		weight int
	}
	items := make([]item, n)
	for i, e := range entries {
		items[i] = item{ip: e.IP, weight: e.Weight}
	}

	for i := 0; i < n; i++ {
		// Calculate total remaining weight
		totalWeight := 0
		for _, it := range items {
			if it.weight > 0 {
				totalWeight += it.weight
			}
		}

		if totalWeight == 0 {
			break
		}

		// Select random point
		r := rand.IntN(totalWeight)
		cumulative := 0

		for j := range items {
			if items[j].weight <= 0 {
				continue
			}
			cumulative += items[j].weight
			if r < cumulative {
				result[i] = items[j].ip
				items[j].weight = 0 // Mark as selected
				break
			}
		}
	}

	// Filter out nil entries
	filtered := make([]net.IP, 0, n)
	for _, ip := range result {
		if ip != nil {
			filtered = append(filtered, ip)
		}
	}

	return filtered
}
```

**Step 4: Run test to verify it passes**

```bash
go test -v ./internal/loadbalance/... -run TestWeightedBalancer
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat(loadbalance): implement weighted random load balancer"
```

---

## Task 7: etcd Storage Interface

**Files:**
- Create: `internal/etcd/storage.go`
- Create: `internal/etcd/client.go`

**Step 1: Define storage interface and types**

Create `internal/etcd/storage.go`:
```go
package etcd

import (
	"context"
)

// StorageMode defines the etcd key organization.
type StorageMode string

const (
	ModeSingle  StorageMode = "single"  // All hosts in one key
	ModePerHost StorageMode = "perhost" // One key per hostname
)

// WatchEvent represents a change event from etcd.
type WatchEvent struct {
	Data    []byte // New data (nil for delete)
	Version int64  // etcd revision
	Err     error  // Error if any
}

// Storage defines the interface for etcd storage operations.
type Storage interface {
	// Load retrieves all hosts data.
	Load(ctx context.Context) ([]byte, int64, error)

	// Watch returns a channel of change events.
	Watch(ctx context.Context) <-chan WatchEvent

	// Close releases resources.
	Close() error
}
```

**Step 2: Create client wrapper**

Create `internal/etcd/client.go`:
```go
package etcd

import (
	"context"
	"crypto/tls"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Config holds etcd connection configuration.
type Config struct {
	Endpoints   []string
	Username    string
	Password    string
	TLSConfig   *tls.Config
	DialTimeout time.Duration
	Key         string      // Base key path
	Mode        StorageMode // Storage mode
}

// Client wraps etcd client with storage operations.
type Client struct {
	client *clientv3.Client
	config *Config
}

// NewClient creates a new etcd client.
func NewClient(cfg *Config) (*Client, error) {
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.Key == "" {
		cfg.Key = "/etcdhosts"
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeSingle
	}

	etcdCfg := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
		Username:    cfg.Username,
		Password:    cfg.Password,
		TLS:         cfg.TLSConfig,
	}

	cli, err := clientv3.New(etcdCfg)
	if err != nil {
		return nil, err
	}

	return &Client{
		client: cli,
		config: cfg,
	}, nil
}

// Storage returns a Storage implementation based on config mode.
func (c *Client) Storage() Storage {
	switch c.config.Mode {
	case ModePerHost:
		return &perHostStorage{client: c.client, key: c.config.Key}
	default:
		return &singleKeyStorage{client: c.client, key: c.config.Key}
	}
}

// Sync synchronizes cluster endpoints.
func (c *Client) Sync(ctx context.Context) error {
	return c.client.Sync(ctx)
}

// Close closes the etcd client.
func (c *Client) Close() error {
	return c.client.Close()
}
```

**Step 3: Implement single key storage**

Add to `internal/etcd/storage.go`:
```go
import (
	"bytes"
	"context"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// singleKeyStorage stores all hosts in a single key.
type singleKeyStorage struct {
	client *clientv3.Client
	key    string
}

func (s *singleKeyStorage) Load(ctx context.Context) ([]byte, int64, error) {
	resp, err := s.client.Get(ctx, s.key)
	if err != nil {
		return nil, 0, err
	}

	if len(resp.Kvs) == 0 {
		return nil, resp.Header.Revision, nil
	}

	return resp.Kvs[0].Value, resp.Kvs[0].Version, nil
}

func (s *singleKeyStorage) Watch(ctx context.Context) <-chan WatchEvent {
	ch := make(chan WatchEvent)

	go func() {
		defer close(ch)

		watchCh := s.client.Watch(clientv3.WithRequireLeader(ctx), s.key)
		for {
			select {
			case <-ctx.Done():
				return
			case resp, ok := <-watchCh:
				if !ok {
					ch <- WatchEvent{Err: context.Canceled}
					return
				}
				if resp.Err() != nil {
					ch <- WatchEvent{Err: resp.Err()}
					continue
				}
				for _, ev := range resp.Events {
					event := WatchEvent{
						Version: ev.Kv.Version,
					}
					if ev.Type != clientv3.EventTypeDelete {
						event.Data = ev.Kv.Value
					}
					ch <- event
				}
			}
		}
	}()

	return ch
}

func (s *singleKeyStorage) Close() error {
	return nil // Client manages connection
}

// perHostStorage stores each hostname in a separate key.
type perHostStorage struct {
	client *clientv3.Client
	key    string // Base key prefix
}

func (s *perHostStorage) Load(ctx context.Context) ([]byte, int64, error) {
	resp, err := s.client.Get(ctx, s.key+"/", clientv3.WithPrefix())
	if err != nil {
		return nil, 0, err
	}

	// Concatenate all values
	var buf bytes.Buffer
	for _, kv := range resp.Kvs {
		buf.Write(kv.Value)
		buf.WriteByte('\n')
	}

	return buf.Bytes(), resp.Header.Revision, nil
}

func (s *perHostStorage) Watch(ctx context.Context) <-chan WatchEvent {
	ch := make(chan WatchEvent)

	go func() {
		defer close(ch)

		watchCh := s.client.Watch(clientv3.WithRequireLeader(ctx), s.key+"/", clientv3.WithPrefix())
		for {
			select {
			case <-ctx.Done():
				return
			case resp, ok := <-watchCh:
				if !ok {
					ch <- WatchEvent{Err: context.Canceled}
					return
				}
				if resp.Err() != nil {
					ch <- WatchEvent{Err: resp.Err()}
					continue
				}
				// On any change, signal reload (full reload for simplicity)
				ch <- WatchEvent{Version: resp.Header.Revision}
			}
		}
	}()

	return ch
}

func (s *perHostStorage) Close() error {
	return nil
}
```

**Step 4: Run go mod tidy**

```bash
go mod tidy
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat(etcd): implement storage interface with single/perhost modes"
```

---

## Task 8: etcd Integration Tests

**Files:**
- Create: `internal/etcd/storage_test.go`

**Step 1: Write integration test with embedded etcd**

Create `internal/etcd/storage_test.go`:
```go
package etcd

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"go.etcd.io/etcd/server/v3/embed"
)

func setupEmbeddedEtcd(t *testing.T) (*embed.Etcd, string) {
	t.Helper()

	dir := t.TempDir()

	cfg := embed.NewConfig()
	cfg.Dir = dir

	// Use random available ports
	lpurl, _ := url.Parse("http://127.0.0.1:0")
	lcurl, _ := url.Parse("http://127.0.0.1:0")
	cfg.ListenPeerUrls = []url.URL{*lpurl}
	cfg.ListenClientUrls = []url.URL{*lcurl}
	cfg.InitialCluster = "default=http://127.0.0.1:0"
	cfg.LogLevel = "error"

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatalf("failed to start embedded etcd: %v", err)
	}

	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		e.Close()
		t.Fatal("etcd took too long to start")
	}

	endpoint := e.Clients[0].Addr().String()
	return e, "http://" + endpoint
}

func TestSingleKeyStorage_LoadAndWatch(t *testing.T) {
	if os.Getenv("ETCD_TEST") == "" {
		t.Skip("skipping etcd integration test; set ETCD_TEST=1 to run")
	}

	etcd, endpoint := setupEmbeddedEtcd(t)
	defer etcd.Close()

	cfg := &Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
		Key:         "/test/hosts",
		Mode:        ModeSingle,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	storage := client.Storage()
	ctx := context.Background()

	// Initially empty
	data, _, err := storage.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %q", data)
	}

	// Put test data
	testData := "192.168.1.1 example.com"
	_, err = client.client.Put(ctx, "/test/hosts", testData)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Load again
	data, _, err = storage.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if string(data) != testData {
		t.Errorf("got %q, want %q", data, testData)
	}

	// Test watch
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	watchCh := storage.Watch(watchCtx)

	// Update data
	newData := "192.168.1.2 new.example.com"
	_, err = client.client.Put(ctx, "/test/hosts", newData)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Should receive watch event
	select {
	case event := <-watchCh:
		if event.Err != nil {
			t.Errorf("watch error: %v", event.Err)
		}
		if string(event.Data) != newData {
			t.Errorf("watch data = %q, want %q", event.Data, newData)
		}
	case <-time.After(5 * time.Second):
		t.Error("watch timeout")
	}
}

func TestPerHostStorage_LoadAndWatch(t *testing.T) {
	if os.Getenv("ETCD_TEST") == "" {
		t.Skip("skipping etcd integration test; set ETCD_TEST=1 to run")
	}

	etcd, endpoint := setupEmbeddedEtcd(t)
	defer etcd.Close()

	cfg := &Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
		Key:         "/test/perhost",
		Mode:        ModePerHost,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	storage := client.Storage()
	ctx := context.Background()

	// Put multiple hosts
	_, _ = client.client.Put(ctx, "/test/perhost/example.com", "192.168.1.1 example.com")
	_, _ = client.client.Put(ctx, "/test/perhost/foo.com", "192.168.1.2 foo.com")

	// Load all
	data, _, err := storage.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should contain both entries
	dataStr := string(data)
	if !contains(dataStr, "192.168.1.1 example.com") {
		t.Errorf("missing example.com entry in %q", dataStr)
	}
	if !contains(dataStr, "192.168.1.2 foo.com") {
		t.Errorf("missing foo.com entry in %q", dataStr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run go mod tidy to add etcd server dependency**

```bash
go get go.etcd.io/etcd/server/v3@v3.6.7
go mod tidy
```

**Step 3: Run integration test**

```bash
ETCD_TEST=1 go test -v ./internal/etcd/... -run TestSingleKeyStorage
ETCD_TEST=1 go test -v ./internal/etcd/... -run TestPerHostStorage
```

Expected: PASS

**Step 4: Commit**

```bash
git add -A
git commit -m "test(etcd): add integration tests with embedded etcd"
```

---

## Task 9: Health Check - TCP Probe

**Files:**
- Create: `internal/healthcheck/probe.go`
- Create: `internal/healthcheck/tcp.go`
- Create: `internal/healthcheck/tcp_test.go`

**Step 1: Define probe interface**

Create `internal/healthcheck/probe.go`:
```go
package healthcheck

import (
	"context"
	"net"
)

// Probe defines the health check probe interface.
type Probe interface {
	// Check performs a health check on the target.
	Check(ctx context.Context, ip net.IP, port int, path string) error
}
```

**Step 2: Write TCP probe test**

Create `internal/healthcheck/tcp_test.go`:
```go
package healthcheck

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPProbe_Success(t *testing.T) {
	// Start a test TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	probe := NewTCPProbe(3 * time.Second)
	ctx := context.Background()

	err = probe.Check(ctx, net.ParseIP("127.0.0.1"), addr.Port, "")
	if err != nil {
		t.Errorf("TCP probe failed: %v", err)
	}
}

func TestTCPProbe_Failure(t *testing.T) {
	probe := NewTCPProbe(1 * time.Second)
	ctx := context.Background()

	// Use a port that's unlikely to be listening
	err := probe.Check(ctx, net.ParseIP("127.0.0.1"), 59999, "")
	if err == nil {
		t.Error("expected TCP probe to fail on closed port")
	}
}

func TestTCPProbe_Timeout(t *testing.T) {
	probe := NewTCPProbe(100 * time.Millisecond)
	ctx := context.Background()

	// Use non-routable IP to trigger timeout
	err := probe.Check(ctx, net.ParseIP("10.255.255.1"), 80, "")
	if err == nil {
		t.Error("expected TCP probe to timeout")
	}
}
```

**Step 3: Run test to verify it fails**

```bash
go test -v ./internal/healthcheck/... -run TestTCPProbe
```

Expected: FAIL with "undefined: NewTCPProbe"

**Step 4: Implement TCP probe**

Create `internal/healthcheck/tcp.go`:
```go
package healthcheck

import (
	"context"
	"fmt"
	"net"
	"time"
)

// TCPProbe implements TCP port connectivity check.
type TCPProbe struct {
	timeout time.Duration
}

// NewTCPProbe creates a new TCP probe with specified timeout.
func NewTCPProbe(timeout time.Duration) *TCPProbe {
	return &TCPProbe{timeout: timeout}
}

// Check verifies TCP connectivity to ip:port.
func (p *TCPProbe) Check(ctx context.Context, ip net.IP, port int, _ string) error {
	addr := fmt.Sprintf("%s:%d", ip.String(), port)

	// Use context deadline if shorter than probe timeout
	timeout := p.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if ctxTimeout := time.Until(deadline); ctxTimeout < timeout {
			timeout = ctxTimeout
		}
	}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp connect failed: %w", err)
	}
	conn.Close()
	return nil
}
```

**Step 5: Run test to verify it passes**

```bash
go test -v ./internal/healthcheck/... -run TestTCPProbe
```

Expected: PASS

**Step 6: Commit**

```bash
git add -A
git commit -m "feat(healthcheck): implement TCP probe"
```

---

## Task 10: Health Check - HTTP Probe

**Files:**
- Create: `internal/healthcheck/http.go`
- Create: `internal/healthcheck/http_test.go`

**Step 1: Write HTTP probe test**

Create `internal/healthcheck/http_test.go`:
```go
package healthcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProbe_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Parse server address
	addr := server.Listener.Addr().(*net.TCPAddr)

	probe := NewHTTPProbe(3*time.Second, false)
	ctx := context.Background()

	err := probe.Check(ctx, net.ParseIP("127.0.0.1"), addr.Port, "/health")
	if err != nil {
		t.Errorf("HTTP probe failed: %v", err)
	}
}

func TestHTTPProbe_BadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	addr := server.Listener.Addr().(*net.TCPAddr)

	probe := NewHTTPProbe(3*time.Second, false)
	ctx := context.Background()

	err := probe.Check(ctx, net.ParseIP("127.0.0.1"), addr.Port, "/")
	if err == nil {
		t.Error("expected HTTP probe to fail on 500 status")
	}
}

func TestHTTPProbe_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	addr := server.Listener.Addr().(*net.TCPAddr)

	probe := NewHTTPProbe(100*time.Millisecond, false)
	ctx := context.Background()

	err := probe.Check(ctx, net.ParseIP("127.0.0.1"), addr.Port, "/")
	if err == nil {
		t.Error("expected HTTP probe to timeout")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test -v ./internal/healthcheck/... -run TestHTTPProbe
```

Expected: FAIL with "undefined: NewHTTPProbe"

**Step 3: Implement HTTP probe**

Create `internal/healthcheck/http.go`:
```go
package healthcheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HTTPProbe implements HTTP/HTTPS health check.
type HTTPProbe struct {
	client *http.Client
	https  bool
}

// NewHTTPProbe creates a new HTTP probe.
func NewHTTPProbe(timeout time.Duration, https bool) *HTTPProbe {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: (&net.Dialer{
			Timeout: timeout,
		}).DialContext,
	}

	return &HTTPProbe{
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		},
		https: https,
	}
}

// Check performs HTTP GET and verifies status code is 2xx or 3xx.
func (p *HTTPProbe) Check(ctx context.Context, ip net.IP, port int, path string) error {
	scheme := "http"
	if p.https {
		scheme = "https"
	}

	if path == "" {
		path = "/"
	}

	url := fmt.Sprintf("%s://%s:%d%s", scheme, ip.String(), port, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("unhealthy status code: %d", resp.StatusCode)
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test -v ./internal/healthcheck/... -run TestHTTPProbe
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat(healthcheck): implement HTTP/HTTPS probe"
```

---

## Task 11: Health Check - ICMP Probe

**Files:**
- Create: `internal/healthcheck/icmp.go`
- Create: `internal/healthcheck/icmp_test.go`

**Step 1: Write ICMP probe test**

Create `internal/healthcheck/icmp_test.go`:
```go
package healthcheck

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

func TestICMPProbe_Localhost(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	probe := NewICMPProbe(3 * time.Second)
	ctx := context.Background()

	err := probe.Check(ctx, net.ParseIP("127.0.0.1"), 0, "")
	if err != nil {
		t.Errorf("ICMP probe to localhost failed: %v", err)
	}
}

func TestICMPProbe_Unreachable(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("ICMP test requires root privileges")
	}

	probe := NewICMPProbe(1 * time.Second)
	ctx := context.Background()

	// Use non-routable IP
	err := probe.Check(ctx, net.ParseIP("10.255.255.1"), 0, "")
	if err == nil {
		t.Error("expected ICMP probe to fail on unreachable host")
	}
}
```

**Step 2: Implement ICMP probe**

Create `internal/healthcheck/icmp.go`:
```go
package healthcheck

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// ICMPProbe implements ICMP echo (ping) health check.
type ICMPProbe struct {
	timeout time.Duration
}

// NewICMPProbe creates a new ICMP probe.
func NewICMPProbe(timeout time.Duration) *ICMPProbe {
	return &ICMPProbe{timeout: timeout}
}

// Check sends ICMP echo request and waits for reply.
func (p *ICMPProbe) Check(ctx context.Context, ip net.IP, _ int, _ string) error {
	var network string
	var proto int
	var msgType icmp.Type

	if ip.To4() != nil {
		network = "ip4:icmp"
		proto = 1 // ICMP for IPv4
		msgType = ipv4.ICMPTypeEcho
	} else {
		network = "ip6:ipv6-icmp"
		proto = 58 // ICMPv6
		msgType = ipv6.ICMPTypeEchoRequest
	}

	conn, err := icmp.ListenPacket(network, "")
	if err != nil {
		return fmt.Errorf("icmp listen failed: %w", err)
	}
	defer conn.Close()

	// Set deadline
	deadline := time.Now().Add(p.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	conn.SetDeadline(deadline)

	// Build ICMP message
	msg := &icmp.Message{
		Type: msgType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("etcdhosts-health-check"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return fmt.Errorf("icmp marshal failed: %w", err)
	}

	// Send
	dst := &net.IPAddr{IP: ip}
	if _, err := conn.WriteTo(msgBytes, dst); err != nil {
		return fmt.Errorf("icmp send failed: %w", err)
	}

	// Receive
	reply := make([]byte, 1500)
	n, _, err := conn.ReadFrom(reply)
	if err != nil {
		return fmt.Errorf("icmp receive failed: %w", err)
	}

	// Parse reply
	parsed, err := icmp.ParseMessage(proto, reply[:n])
	if err != nil {
		return fmt.Errorf("icmp parse failed: %w", err)
	}

	switch parsed.Type {
	case ipv4.ICMPTypeEchoReply, ipv6.ICMPTypeEchoReply:
		return nil
	default:
		return fmt.Errorf("unexpected icmp type: %v", parsed.Type)
	}
}
```

**Step 3: Add icmp dependency**

```bash
go get golang.org/x/net/icmp
go mod tidy
```

**Step 4: Commit**

```bash
git add -A
git commit -m "feat(healthcheck): implement ICMP probe"
```

---

## Task 12: Health Check Scheduler

**Files:**
- Create: `internal/healthcheck/checker.go`
- Create: `internal/healthcheck/cache.go`

**Step 1: Implement cache**

Create `internal/healthcheck/cache.go`:
```go
package healthcheck

import (
	"sync"
	"time"
)

// CacheEntry holds health status for a single IP.
type CacheEntry struct {
	Healthy     bool
	LastCheck   time.Time
	Failures    int // Consecutive failures
	Successes   int // Consecutive successes
}

// Cache stores health check results.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry // key: "hostname:ip"
	ttl     time.Duration
}

// NewCache creates a new health cache.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
	}
}

// Get returns the health status for a key.
func (c *Cache) Get(key string) (healthy bool, found bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return true, false // Default healthy if not found
	}

	// Check if expired
	if time.Since(entry.LastCheck) > c.ttl {
		return entry.Healthy, false // Return old value but mark as not found
	}

	return entry.Healthy, true
}

// Update records a health check result.
func (c *Cache) Update(key string, healthy bool, failuresBeforeDown, successBeforeUp int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		entry = &CacheEntry{Healthy: true}
		c.entries[key] = entry
	}

	entry.LastCheck = time.Now()

	if healthy {
		entry.Failures = 0
		entry.Successes++
		if entry.Successes >= successBeforeUp {
			entry.Healthy = true
		}
	} else {
		entry.Successes = 0
		entry.Failures++
		if entry.Failures >= failuresBeforeDown {
			entry.Healthy = false
		}
	}
}

// IsHealthy returns current health status.
func (c *Cache) IsHealthy(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return true // Default healthy
	}
	return entry.Healthy
}

// Keys returns all cached keys.
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	return keys
}
```

**Step 2: Implement checker**

Create `internal/healthcheck/checker.go`:
```go
package healthcheck

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/etcdhosts/etcdhosts/internal/hosts"
)

// Config holds health check configuration.
type Config struct {
	Interval           time.Duration
	Timeout            time.Duration
	MaxConcurrent      int
	CacheTTL           time.Duration
	FailuresBeforeDown int
	SuccessBeforeUp    int
	UnhealthyPolicy    UnhealthyPolicy
}

// UnhealthyPolicy defines behavior when all IPs are unhealthy.
type UnhealthyPolicy string

const (
	PolicyReturnAll    UnhealthyPolicy = "return_all"
	PolicyReturnEmpty  UnhealthyPolicy = "return_empty"
	PolicyFallthrough  UnhealthyPolicy = "fallthrough"
)

// DefaultConfig returns default health check config.
func DefaultConfig() *Config {
	return &Config{
		Interval:           10 * time.Second,
		Timeout:            3 * time.Second,
		MaxConcurrent:      10,
		CacheTTL:           20 * time.Second, // interval * 2
		FailuresBeforeDown: 3,
		SuccessBeforeUp:    1,
		UnhealthyPolicy:    PolicyReturnAll,
	}
}

// Checker manages health checks for all registered targets.
type Checker struct {
	config    *Config
	cache     *Cache
	probes    map[hosts.CheckType]Probe
	targets   []Target
	targetsMu sync.RWMutex
	cancel    context.CancelFunc
}

// Target represents a health check target.
type Target struct {
	Hostname string
	IP       net.IP
	Health   *hosts.Health
}

// NewChecker creates a new health checker.
func NewChecker(cfg *Config) *Checker {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	return &Checker{
		config: cfg,
		cache:  NewCache(cfg.CacheTTL),
		probes: map[hosts.CheckType]Probe{
			hosts.CheckTCP:   NewTCPProbe(cfg.Timeout),
			hosts.CheckHTTP:  NewHTTPProbe(cfg.Timeout, false),
			hosts.CheckHTTPS: NewHTTPProbe(cfg.Timeout, true),
			hosts.CheckICMP:  NewICMPProbe(cfg.Timeout),
		},
	}
}

// UpdateTargets replaces the list of health check targets.
func (c *Checker) UpdateTargets(records []hosts.Record) {
	var targets []Target

	for _, r := range records {
		if r.Health != nil {
			targets = append(targets, Target{
				Hostname: r.Hostname,
				IP:       r.IP,
				Health:   r.Health,
			})
		}
	}

	c.targetsMu.Lock()
	c.targets = targets
	c.targetsMu.Unlock()
}

// Start begins periodic health checking.
func (c *Checker) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(c.config.Interval)
		defer ticker.Stop()

		// Initial check
		c.checkAll(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.checkAll(ctx)
			}
		}
	}()
}

// Stop stops the health checker.
func (c *Checker) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

// IsHealthy returns whether the target is healthy.
func (c *Checker) IsHealthy(hostname string, ip net.IP) bool {
	key := fmt.Sprintf("%s:%s", hostname, ip.String())
	return c.cache.IsHealthy(key)
}

// checkAll runs health checks on all targets.
func (c *Checker) checkAll(ctx context.Context) {
	c.targetsMu.RLock()
	targets := make([]Target, len(c.targets))
	copy(targets, c.targets)
	c.targetsMu.RUnlock()

	sem := make(chan struct{}, c.config.MaxConcurrent)
	var wg sync.WaitGroup

	for _, t := range targets {
		wg.Add(1)
		go func(target Target) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			c.checkOne(ctx, target)
		}(t)
	}

	wg.Wait()
}

// checkOne performs a single health check.
func (c *Checker) checkOne(ctx context.Context, target Target) {
	probe, ok := c.probes[target.Health.Type]
	if !ok {
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	err := probe.Check(checkCtx, target.IP, target.Health.Port, target.Health.Path)
	healthy := err == nil

	key := fmt.Sprintf("%s:%s", target.Hostname, target.IP.String())
	c.cache.Update(key, healthy, c.config.FailuresBeforeDown, c.config.SuccessBeforeUp)
}
```

**Step 3: Run tests**

```bash
go test -v ./internal/healthcheck/...
```

Expected: PASS

**Step 4: Commit**

```bash
git add -A
git commit -m "feat(healthcheck): implement checker scheduler with cache"
```

---

## Task 13: CoreDNS Plugin Entry

**Files:**
- Create: `plugin.go`
- Create: `setup.go`
- Create: `handler.go`
- Create: `metrics.go`

**Step 1: Create plugin.go**

Create `plugin.go`:
```go
package etcdhosts

import (
	"github.com/coredns/coredns/plugin"
)

const pluginName = "etcdhosts"

func init() {
	plugin.Register(pluginName, setup)
}
```

**Step 2: Create metrics.go**

Create `metrics.go`:
```go
package etcdhosts

import (
	"github.com/coredns/coredns/plugin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	entriesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "entries_total",
		Help:      "Total number of hosts entries loaded.",
	})

	queriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "queries_total",
		Help:      "Total DNS queries handled.",
	}, []string{"qtype", "result"})

	queryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "query_duration_seconds",
		Help:      "DNS query processing duration.",
		Buckets:   []float64{.0001, .0005, .001, .005, .01, .05},
	}, []string{"qtype"})

	etcdSyncTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "etcd_sync_total",
		Help:      "Total etcd sync operations.",
	}, []string{"status"})

	etcdLastSync = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "etcd_last_sync_timestamp",
		Help:      "Timestamp of last successful etcd sync.",
	})

	healthcheckStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "healthcheck_status",
		Help:      "Health status of each IP (1=healthy, 0=unhealthy).",
	}, []string{"hostname", "ip"})
)
```

**Step 3: Create handler.go**

Create `handler.go`:
```go
package etcdhosts

import (
	"context"
	"net"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"

	"github.com/etcdhosts/etcdhosts/internal/healthcheck"
	"github.com/etcdhosts/etcdhosts/internal/hosts"
	"github.com/etcdhosts/etcdhosts/internal/loadbalance"
)

// EtcdHosts is the plugin handler.
type EtcdHosts struct {
	Next plugin.Handler

	Origins []string
	Fall    fall.F
	TTL     uint32

	store    *hosts.Store
	checker  *healthcheck.Checker
	balancer *loadbalance.WeightedBalancer
}

// ServeDNS implements the plugin.Handler interface.
func (h *EtcdHosts) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	qname := state.Name()
	qtype := state.QType()

	start := time.Now()
	defer func() {
		queryDuration.WithLabelValues(dns.TypeToString[qtype]).Observe(time.Since(start).Seconds())
	}()

	// Check zone match
	zone := plugin.Zones(h.Origins).Matches(qname)
	if zone == "" && qtype != dns.TypePTR {
		return plugin.NextOrFailure(h.Name(), h.Next, ctx, w, r)
	}

	var answers []dns.RR

	switch qtype {
	case dns.TypeA:
		answers = h.handleA(qname)
	case dns.TypeAAAA:
		answers = h.handleAAAA(qname)
	case dns.TypePTR:
		answers = h.handlePTR(qname)
	}

	// Record metrics
	if len(answers) > 0 {
		queriesTotal.WithLabelValues(dns.TypeToString[qtype], "hit").Inc()
	} else {
		queriesTotal.WithLabelValues(dns.TypeToString[qtype], "miss").Inc()
	}

	// Handle no answers
	if len(answers) == 0 {
		if h.Fall.Through(qname) {
			return plugin.NextOrFailure(h.Name(), h.Next, ctx, w, r)
		}
		return dns.RcodeServerFailure, nil
	}

	// Build response
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = answers

	_ = w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

func (h *EtcdHosts) handleA(qname string) []dns.RR {
	entries := h.store.LookupV4WithWildcard(qname)
	if len(entries) == 0 {
		return nil
	}

	// Build balancer entries with health status
	balancerEntries := h.buildBalancerEntries(qname, entries)
	ips := h.balancer.Select(balancerEntries)

	return h.buildARecords(qname, ips, entries)
}

func (h *EtcdHosts) handleAAAA(qname string) []dns.RR {
	entries := h.store.LookupV6WithWildcard(qname)
	if len(entries) == 0 {
		return nil
	}

	balancerEntries := h.buildBalancerEntries(qname, entries)
	ips := h.balancer.Select(balancerEntries)

	return h.buildAAAARecords(qname, ips, entries)
}

func (h *EtcdHosts) handlePTR(qname string) []dns.RR {
	addr := dnsutil.ExtractAddressFromReverse(qname)
	if addr == "" {
		return nil
	}

	hostnames := h.store.LookupAddr(addr)
	if len(hostnames) == 0 {
		return nil
	}

	answers := make([]dns.RR, len(hostnames))
	for i, hostname := range hostnames {
		rr := &dns.PTR{
			Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: h.TTL},
			Ptr: hostname,
		}
		answers[i] = rr
	}
	return answers
}

func (h *EtcdHosts) buildBalancerEntries(hostname string, entries []hosts.Entry) []loadbalance.Entry {
	result := make([]loadbalance.Entry, len(entries))
	for i, e := range entries {
		healthy := true
		if e.Health != nil && h.checker != nil {
			healthy = h.checker.IsHealthy(hostname, e.IP)
		}
		result[i] = loadbalance.Entry{
			IP:      e.IP,
			Weight:  e.Weight,
			Healthy: healthy,
		}
	}
	return result
}

func (h *EtcdHosts) buildARecords(qname string, ips []net.IP, entries []hosts.Entry) []dns.RR {
	answers := make([]dns.RR, 0, len(ips))
	for _, ip := range ips {
		ttl := h.TTL
		// Find entry-specific TTL
		for _, e := range entries {
			if e.IP.Equal(ip) && e.TTL > 0 {
				ttl = e.TTL
				break
			}
		}
		rr := &dns.A{
			Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
			A:   ip,
		}
		answers = append(answers, rr)
	}
	return answers
}

func (h *EtcdHosts) buildAAAARecords(qname string, ips []net.IP, entries []hosts.Entry) []dns.RR {
	answers := make([]dns.RR, 0, len(ips))
	for _, ip := range ips {
		ttl := h.TTL
		for _, e := range entries {
			if e.IP.Equal(ip) && e.TTL > 0 {
				ttl = e.TTL
				break
			}
		}
		rr := &dns.AAAA{
			Hdr:  dns.RR_Header{Name: qname, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl},
			AAAA: ip,
		}
		answers = append(answers, rr)
	}
	return answers
}

// Name implements the plugin.Handler interface.
func (h *EtcdHosts) Name() string {
	return pluginName
}
```

**Step 4: Create setup.go** (abbreviated - full implementation needed)

Create `setup.go`:
```go
package etcdhosts

import (
	"context"
	"strconv"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	mwtls "github.com/coredns/coredns/plugin/pkg/tls"

	"github.com/etcdhosts/etcdhosts/internal/etcd"
	"github.com/etcdhosts/etcdhosts/internal/healthcheck"
	"github.com/etcdhosts/etcdhosts/internal/hosts"
	"github.com/etcdhosts/etcdhosts/internal/loadbalance"
)

var log = clog.NewWithPlugin(pluginName)

func setup(c *caddy.Controller) error {
	h, etcdClient, err := parseConfig(c)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	// Setup lifecycle
	ctx, cancel := context.WithCancel(context.Background())

	c.OnStartup(func() error {
		// Initial load
		if err := h.loadFromEtcd(ctx, etcdClient); err != nil {
			log.Warningf("initial etcd load failed: %v", err)
		}
		// Start watching
		go h.watchEtcd(ctx, etcdClient)
		// Start health checker
		if h.checker != nil {
			h.checker.Start(ctx)
		}
		return nil
	})

	c.OnShutdown(func() error {
		cancel()
		if h.checker != nil {
			h.checker.Stop()
		}
		return etcdClient.Close()
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		h.Next = next
		return h
	})

	return nil
}

func parseConfig(c *caddy.Controller) (*EtcdHosts, *etcd.Client, error) {
	h := &EtcdHosts{
		TTL:      3600,
		store:    hosts.NewStore(),
		balancer: loadbalance.NewWeightedBalancer(),
	}

	etcdCfg := &etcd.Config{
		DialTimeout: 5 * time.Second,
		Key:         "/etcdhosts",
		Mode:        etcd.ModeSingle,
	}

	var hcCfg *healthcheck.Config

	for c.Next() {
		h.Origins = plugin.OriginsFromArgsOrServerBlock(c.RemainingArgs(), c.ServerBlockKeys)

		for c.NextBlock() {
			switch c.Val() {
			case "endpoint":
				etcdCfg.Endpoints = c.RemainingArgs()
			case "credentials":
				args := c.RemainingArgs()
				if len(args) != 2 {
					return nil, nil, c.ArgErr()
				}
				etcdCfg.Username, etcdCfg.Password = args[0], args[1]
			case "tls":
				tlsCfg, err := mwtls.NewTLSConfigFromArgs(c.RemainingArgs()...)
				if err != nil {
					return nil, nil, err
				}
				etcdCfg.TLSConfig = tlsCfg
			case "timeout":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, nil, c.ArgErr()
				}
				d, err := time.ParseDuration(args[0])
				if err != nil {
					return nil, nil, err
				}
				etcdCfg.DialTimeout = d
			case "storage":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, nil, c.ArgErr()
				}
				etcdCfg.Mode = etcd.StorageMode(args[0])
			case "key":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, nil, c.ArgErr()
				}
				etcdCfg.Key = args[0]
			case "ttl":
				args := c.RemainingArgs()
				if len(args) != 1 {
					return nil, nil, c.ArgErr()
				}
				ttl, err := strconv.Atoi(args[0])
				if err != nil || ttl <= 0 {
					return nil, nil, c.Errf("invalid ttl: %s", args[0])
				}
				h.TTL = uint32(ttl)
			case "fallthrough":
				h.Fall.SetZonesFromArgs(c.RemainingArgs())
			case "healthcheck":
				hcCfg = healthcheck.DefaultConfig()
				for c.NextBlock() {
					if err := parseHealthcheckConfig(c, hcCfg); err != nil {
						return nil, nil, err
					}
				}
			}
		}
	}

	// Create etcd client
	client, err := etcd.NewClient(etcdCfg)
	if err != nil {
		return nil, nil, err
	}

	// Create health checker if configured
	if hcCfg != nil {
		h.checker = healthcheck.NewChecker(hcCfg)
	}

	return h, client, nil
}

func parseHealthcheckConfig(c *caddy.Controller, cfg *healthcheck.Config) error {
	switch c.Val() {
	case "interval":
		args := c.RemainingArgs()
		if len(args) != 1 {
			return c.ArgErr()
		}
		d, err := time.ParseDuration(args[0])
		if err != nil {
			return err
		}
		cfg.Interval = d
	case "timeout":
		args := c.RemainingArgs()
		if len(args) != 1 {
			return c.ArgErr()
		}
		d, err := time.ParseDuration(args[0])
		if err != nil {
			return err
		}
		cfg.Timeout = d
	case "max_concurrent":
		args := c.RemainingArgs()
		if len(args) != 1 {
			return c.ArgErr()
		}
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		cfg.MaxConcurrent = n
	case "unhealthy_policy":
		args := c.RemainingArgs()
		if len(args) != 1 {
			return c.ArgErr()
		}
		cfg.UnhealthyPolicy = healthcheck.UnhealthyPolicy(args[0])
	}
	return nil
}

func (h *EtcdHosts) loadFromEtcd(ctx context.Context, client *etcd.Client) error {
	storage := client.Storage()
	data, _, err := storage.Load(ctx)
	if err != nil {
		return err
	}

	parser := hosts.NewParser()
	records, err := parser.Parse(data)
	if err != nil {
		return err
	}

	h.store.Update(records)
	entriesTotal.Set(float64(len(records)))
	etcdLastSync.SetToCurrentTime()
	etcdSyncTotal.WithLabelValues("success").Inc()

	if h.checker != nil {
		h.checker.UpdateTargets(records)
	}

	return nil
}

func (h *EtcdHosts) watchEtcd(ctx context.Context, client *etcd.Client) {
	storage := client.Storage()
	watchCh := storage.Watch(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watchCh:
			if !ok {
				return
			}
			if event.Err != nil {
				log.Errorf("etcd watch error: %v", event.Err)
				etcdSyncTotal.WithLabelValues("error").Inc()
				continue
			}
			log.Info("etcd hosts updated, reloading...")
			if err := h.loadFromEtcd(ctx, client); err != nil {
				log.Errorf("reload failed: %v", err)
				etcdSyncTotal.WithLabelValues("error").Inc()
			}
		}
	}
}
```

**Step 5: Run go mod tidy and build**

```bash
go mod tidy
go build ./...
```

Expected: Build succeeds

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: implement CoreDNS plugin with full functionality"
```

---

## Task 14: Full Test Build

**Step 1: Run all tests**

```bash
go test -v -race ./...
```

**Step 2: Run integration tests**

```bash
ETCD_TEST=1 go test -v ./internal/etcd/...
```

**Step 3: Build with CoreDNS**

```bash
task all
```

Expected: CoreDNS binary with etcdhosts plugin in dist/

**Step 4: Commit**

```bash
git add -A
git commit -m "chore: verify full build and tests pass"
```

---

## Summary

This implementation plan provides 14 tasks covering:

1. **Tasks 1-5**: Core hosts parsing, storage, and wildcard matching
2. **Task 6**: Weighted load balancing
3. **Tasks 7-8**: etcd storage with integration tests
4. **Tasks 9-12**: Health checking (TCP, HTTP, ICMP) with scheduler
5. **Tasks 13-14**: CoreDNS plugin integration and final build

Each task follows TDD with explicit test-first approach and frequent commits.
