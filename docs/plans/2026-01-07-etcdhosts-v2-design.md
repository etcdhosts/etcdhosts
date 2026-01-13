# etcdhosts v2 Design Document

## Overview

A complete rewrite of the etcdhosts CoreDNS plugin for centralized hosts management via etcd.

## Goals

- Improve code quality and maintainability
- Upgrade to modern Go (1.25+) and latest CoreDNS/etcd versions
- Add advanced DNS features (wildcards, weighted load balancing, health checks)
- Enhanced observability with Prometheus metrics

## Tech Stack

| Component | Choice |
|-----------|--------|
| Go Version | 1.25+ |
| CoreDNS | Latest stable (follow upstream) |
| etcd Client | Official etcd/client/v3 |
| Build Tool | Taskfile |

## Project Structure

```
etcdhosts/
├── cmd/
│   └── coredns/              # CoreDNS build entry (optional)
├── internal/
│   ├── hosts/                # Hosts parsing core
│   │   ├── parser.go         # Extended hosts format parser
│   │   ├── record.go         # Record data structures
│   │   ├── wildcard.go       # Wildcard matching logic
│   │   └── store.go          # In-memory storage and lookup
│   ├── etcd/                 # etcd client wrapper
│   │   ├── client.go         # Connection management
│   │   ├── watcher.go        # Watch mechanism
│   │   └── storage.go        # single/perhost storage strategies
│   ├── healthcheck/          # Health checking
│   │   ├── checker.go        # Check scheduler
│   │   ├── cache.go          # Health status cache
│   │   ├── tcp.go            # TCP probe
│   │   ├── http.go           # HTTP/HTTPS probe
│   │   └── icmp.go           # ICMP ping
│   └── loadbalance/          # Load balancing
│       └── weighted.go       # Weighted random selection
├── plugin.go                 # CoreDNS plugin entry
├── setup.go                  # Configuration parsing
├── handler.go                # DNS request handling
├── metrics.go                # Prometheus metrics
├── Taskfile.yaml             # Build script
└── go.mod
```

## Data Structures

### Record

```go
type Record struct {
    Hostname  string
    IPs       []net.IP
    TTL       uint32        // DNS TTL (seconds), 0 = use default
    Weight    int           // Weight for load balancing, default 1
    Health    *HealthCheck  // Health check config, nil = disabled
}

type HealthCheck struct {
    Type     CheckType     // tcp / http / https / icmp
    Port     int           // Target port
    Path     string        // URL path for HTTP/HTTPS
    Interval time.Duration // Check interval, default 10s
    Timeout  time.Duration // Timeout, default 3s
}

type CheckType string

const (
    CheckTCP   CheckType = "tcp"
    CheckHTTP  CheckType = "http"
    CheckHTTPS CheckType = "https"
    CheckICMP  CheckType = "icmp"
)
```

### Extended Hosts Format

```
# Basic record
192.168.1.1 gateway.local

# With weight and TTL
192.168.1.10 api.example.com # +etcdhosts weight=10 ttl=60

# With TCP health check
192.168.1.20 db.example.com # +etcdhosts hc=tcp:3306

# With HTTP health check
192.168.1.30 web.example.com # +etcdhosts weight=5 hc=http:8080/health

# Wildcard + HTTPS check
192.168.1.40 *.api.example.com # +etcdhosts hc=https:443/ping ttl=30

# Multi-IP load balancing (same hostname, multiple lines)
10.0.0.1 lb.example.com # +etcdhosts weight=3 hc=http:80/status
10.0.0.2 lb.example.com # +etcdhosts weight=2 hc=http:80/status
10.0.0.3 lb.example.com # +etcdhosts weight=1 hc=http:80/status
```

### Validation Rules

1. Same hostname with multiple records must use consistent health check type
2. Single record ignores weight (returns directly)
3. Weight is proportional (not percentage-based)

## etcd Storage

### Storage Modes

**Single Key Mode (default):**
```
/etcdhosts -> "full hosts content"
```

**Per-Host Mode:**
```
/etcdhosts/hosts/example.com -> "10.0.0.1 example.com # +etcdhosts weight=3\n10.0.0.2 example.com"
/etcdhosts/hosts/api.example.com -> "192.168.1.10 api.example.com # +etcdhosts ttl=30"
/etcdhosts/hosts/*.example.com -> "192.168.1.100 *.example.com"
```

### Storage Interface

```go
type StorageMode string

const (
    ModeSingleKey StorageMode = "single"
    ModePerHost   StorageMode = "perhost"
)

type Storage interface {
    Load(ctx context.Context) ([]byte, int64, error)
    Watch(ctx context.Context) <-chan WatchEvent
    Export(ctx context.Context) ([]byte, error)
}
```

## Wildcard Matching

Priority order (highest to lowest):

1. Exact match: `foo.example.com`
2. Longest wildcard: `*.sub.example.com`
3. Shorter wildcard: `*.example.com`

## Load Balancing

### Weighted Random Algorithm

```
Input:
  IP-A weight=3 (healthy)
  IP-B weight=2 (healthy)
  IP-C weight=1 (unhealthy, filtered)

Processing:
  Total weight = 3 + 2 = 5
  IP-A probability = 3/5 = 60%
  IP-B probability = 2/5 = 40%

Output (DNS response):
  60% cases: [IP-A, IP-B]  <- A first
  40% cases: [IP-B, IP-A]  <- B first
```

### Single Record Optimization

```go
if len(records) == 1 {
    return []net.IP{records[0].IP}  // Skip weight calculation
}
```

## Health Check

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Checker Scheduler                     │
├─────────────────────────────────────────────────────────┤
│  Every interval:                                        │
│  1. Iterate all records with health check enabled       │
│  2. Execute probes concurrently (limited by max_concurrent) │
│  3. Update status map                                   │
│  4. Trigger metrics update                              │
└─────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────┐
│                    DNS Query Time                        │
├─────────────────────────────────────────────────────────┤
│  1. Get all IPs for hostname                            │
│  2. Filter out unhealthy IPs                            │
│  3. Apply weighted load balancing on remaining IPs      │
│  4. If all unhealthy, apply unhealthy_policy            │
└─────────────────────────────────────────────────────────┘
```

### Cache Mechanism

```go
type HealthCache struct {
    mu      sync.RWMutex
    entries map[string]*CacheEntry  // key: "hostname:ip"
    ttl     time.Duration           // Cache TTL, default = interval * 2
}

type CacheEntry struct {
    Healthy   bool
    LastCheck time.Time
    ExpiresAt time.Time
    Failures  int  // Consecutive failures
}

type StateTransition struct {
    FailuresBeforeDown int  // Mark unhealthy after N failures, default 3
    SuccessBeforeUp    int  // Mark healthy after N successes, default 1
}
```

### Cache Flow

**Read:**
1. Cache hit and not expired -> use cached value
2. Cache miss or expired -> use old value + trigger async check
3. Never checked -> default healthy (optimistic)

**Write:**
1. Update CacheEntry on check completion
2. Apply state transition (debounce)
3. Set new ExpiresAt

### Unhealthy Policy

| Policy | Behavior | Use Case |
|--------|----------|----------|
| `return_all` | Return all IPs (ignore health) | Prefer bad response over empty |
| `return_empty` | Return NODATA | Strict mode, client has fallback |
| `fallthrough` | Pass to next CoreDNS plugin | Has fallback DNS source |

## Configuration Format

```
etcdhosts [ZONES...] {
    # etcd connection
    endpoint <URL...>                    # etcd endpoints (required)
    credentials <USER> <PASSWORD>        # Authentication (optional)
    tls <CERT> <KEY> <CA>               # TLS config (optional)
    timeout <DURATION>                   # Connection timeout, default 3s

    # Storage
    storage <single|perhost>             # Storage mode, default single
    key <PATH>                           # etcd key path, default /etcdhosts

    # DNS
    ttl <SECONDS>                        # Default TTL, default 3600
    fallthrough [ZONES...]               # Pass unmatched to next plugin

    # Health check (optional block)
    healthcheck {
        interval <DURATION>              # Check interval, default 10s
        timeout <DURATION>               # Probe timeout, default 3s
        max_concurrent <N>               # Max concurrent checks, default 10
        cache_ttl <DURATION>             # Cache TTL, default interval*2
        failures_before_down <N>         # Failures to mark unhealthy, default 3
        success_before_up <N>            # Successes to mark healthy, default 1
        unhealthy_policy <POLICY>        # return_all|return_empty|fallthrough
    }

    # Inline hosts (optional, higher priority than etcd)
    inline {
        <IP> <HOSTNAME> [# +etcdhosts ...]
    }
}
```

### Example Configuration

```
etcdhosts example.com internal.local {
    endpoint https://etcd1:2379 https://etcd2:2379 https://etcd3:2379
    credentials admin secret123
    tls /etc/ssl/etcd.crt /etc/ssl/etcd.key /etc/ssl/ca.crt
    timeout 5s

    storage single
    key /dns/hosts

    ttl 300
    fallthrough .

    healthcheck {
        interval 15s
        timeout 5s
        max_concurrent 20
        failures_before_down 2
        unhealthy_policy return_all
    }

    inline {
        127.0.0.1 localhost
        192.168.1.1 gateway.internal.local # +etcdhosts ttl=60
    }
}
```

## Prometheus Metrics

```go
// Record count
coredns_etcdhosts_entries_total

// DNS query stats
coredns_etcdhosts_queries_total{qtype="A|AAAA|PTR", result="hit|miss|error"}
coredns_etcdhosts_query_duration_seconds{qtype="A|AAAA|PTR"}

// etcd sync
coredns_etcdhosts_etcd_sync_total{status="success|error"}
coredns_etcdhosts_etcd_last_sync_timestamp

// Health check
coredns_etcdhosts_healthcheck_status{hostname="...", ip="..."}  // 1=healthy, 0=unhealthy
coredns_etcdhosts_healthcheck_duration_seconds{type="tcp|http|https|icmp"}
coredns_etcdhosts_healthcheck_cache_hits_total
coredns_etcdhosts_healthcheck_cache_misses_total
```

## Testing Strategy

### Test Pyramid

```
                    ┌─────────┐
                    │  E2E    │  CoreDNS integration
                   ─┴─────────┴─
                ┌────────────────┐
                │  Integration   │  etcd integration
               ─┴────────────────┴─
            ┌───────────────────────┐
            │      Unit Tests       │  Pure logic
           ─┴───────────────────────┴─
```

### Unit Tests (no external dependencies)

- `internal/hosts/parser_test.go` - Hosts format parsing
- `internal/hosts/wildcard_test.go` - Wildcard matching
- `internal/hosts/store_test.go` - In-memory storage
- `internal/loadbalance/weighted_test.go` - Weighted algorithm

### Integration Tests (embedded etcd)

```go
import "go.etcd.io/etcd/server/v3/embed"

func setupEmbeddedEtcd(t *testing.T) (*embed.Etcd, *clientv3.Client)

func TestStorage_SingleKey_Watch(t *testing.T)
func TestStorage_PerHost_CRUD(t *testing.T)
```

### Health Check Tests (testcontainers)

```go
import "github.com/testcontainers/testcontainers-go"

func TestHTTPProbe_RealServer(t *testing.T)
func TestTCPProbe_RealServer(t *testing.T)
```

### E2E Tests (full CoreDNS)

```go
func TestCoreDNS_Integration(t *testing.T) {
    // 1. Start embedded etcd
    // 2. Write test hosts data
    // 3. Start CoreDNS with etcdhosts plugin
    // 4. Send DNS queries and verify responses
    // 5. Modify etcd data, verify auto-update
}
```

### Coverage Targets

| Package | Coverage |
|---------|----------|
| internal/hosts | > 90% |
| internal/loadbalance | > 90% |
| internal/healthcheck | > 80% |
| internal/etcd | > 80% |
| plugin (handler) | > 70% |

## Implementation Phases

### Phase 1: Core Foundation
- Project scaffold (go.mod, directory structure, Taskfile)
- Hosts parser (extended format `# +etcdhosts`)
- In-memory storage and lookup (with wildcard matching)
- Unit tests

### Phase 2: etcd Integration
- etcd client wrapper
- Single key / per-host storage strategies
- Watch mechanism
- Embedded etcd integration tests

### Phase 3: CoreDNS Plugin
- Plugin entry and config parsing
- DNS request handling (A/AAAA/PTR)
- Fallthrough support
- Basic Prometheus metrics

### Phase 4: Advanced Features
- Weighted load balancing
- Health checks (TCP/HTTP/HTTPS/ICMP)
- Health status cache with debouncing
- Full metrics and testcontainers tests

### Phase 5: Finalization
- E2E tests
- Documentation (README, config examples)
- Taskfile refinement (build, test, release)
