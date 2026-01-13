# etcdhosts

> etcdhosts is a CoreDNS plugin that stores hosts configuration in etcd for centralized management and multi-node consistency.

## Features

**v2.0** brings a complete rewrite with the following features:

- **Wildcard Support** - Match `*.example.com` patterns with priority-based resolution
- **Weighted Load Balancing** - Distribute traffic across multiple backends with configurable weights
- **Health Checking** - TCP, HTTP, HTTPS, and ICMP probes with configurable thresholds
- **Extended Hosts Format** - Configure TTL, weight, and health checks inline with `# +etcdhosts`
- **Storage Modes** - Single key (default) or per-host key storage
- **Prometheus Metrics** - Query counts, latency, health status, and sync metrics
- **IPv4/IPv6 Support** - Full dual-stack support with reverse DNS lookups

## Requirements

- Go 1.25+
- CoreDNS v1.14.0+
- etcd v3.5+
- [go-task](https://taskfile.dev/installation/)
- GNU sed (macOS users: `brew install gnu-sed`)

## Installation

```sh
# Clone and build
git clone https://github.com/etcdhosts/etcdhosts.git
cd etcdhosts
task all

# Binary will be in dist/
ls dist/
```

## Configuration

### Basic Configuration

```
etcdhosts [ZONES...] {
    endpoint ETCD_ENDPOINT...
    key ETCD_KEY
    [fallthrough [ZONES...]]
}
```

### Full Configuration

```
etcdhosts [ZONES...] {
    endpoint ETCD_ENDPOINT...
    key ETCD_KEY
    storage single|perhost
    ttl SECONDS
    timeout DURATION
    credentials USERNAME PASSWORD
    tls CERT [KEY] [CA]
    fallthrough [ZONES...]

    healthcheck {
        interval DURATION
        timeout DURATION
        max_concurrent N
        unhealthy_policy return_all|return_empty|fallthrough
    }
}
```

### Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `endpoint` | (required) | etcd endpoint(s), space-separated |
| `key` | `/etcdhosts` | etcd key or prefix |
| `storage` | `single` | Storage mode: `single` (one key) or `perhost` (key per host) |
| `ttl` | `3600` | Default DNS TTL in seconds |
| `timeout` | `5s` | etcd connection timeout |
| `credentials` | - | etcd username and password |
| `tls` | - | TLS certificate, key, and CA files |
| `fallthrough` | - | Pass to next plugin if no match |

### Health Check Options

| Option | Default | Description |
|--------|---------|-------------|
| `interval` | `10s` | Check interval |
| `timeout` | `3s` | Check timeout |
| `max_concurrent` | `10` | Maximum concurrent checks |
| `unhealthy_policy` | `return_all` | Policy when all backends are unhealthy |

### Example Corefile

```
. {
    etcdhosts {
        endpoint https://etcd1:2379 https://etcd2:2379 https://etcd3:2379
        key /etcdhosts
        tls /etc/ssl/etcd.pem /etc/ssl/etcd-key.pem /etc/ssl/ca.pem
        fallthrough

        healthcheck {
            interval 10s
            timeout 3s
            unhealthy_policy return_all
        }
    }

    forward . 8.8.8.8 8.8.4.4
    log
    errors
}
```

## Data Format

### Basic Format

Standard `/etc/hosts` format:

```
192.168.1.1 host1.example.com
192.168.1.2 host2.example.com host2
2001:db8::1 ipv6host.example.com
```

### Extended Format

Use `# +etcdhosts` marker for advanced features:

```
# Basic entry
192.168.1.1 host1.example.com

# With weight and TTL
192.168.1.2 host2.example.com # +etcdhosts weight=10 ttl=300

# With TCP health check
192.168.1.3 db.example.com # +etcdhosts hc=tcp:3306

# With HTTP health check
192.168.1.4 api.example.com # +etcdhosts weight=5 hc=http:8080:/health

# With HTTPS health check
192.168.1.5 secure.example.com # +etcdhosts hc=https:443:/healthz

# With ICMP health check (requires root)
192.168.1.6 server.example.com # +etcdhosts hc=icmp

# Wildcard entry
192.168.1.10 *.apps.example.com # +etcdhosts weight=1

# Full example with all options
192.168.1.100 prod.example.com # +etcdhosts weight=100 ttl=60 hc=http:80:/status
```

### Extended Format Options

| Option | Format | Description |
|--------|--------|-------------|
| `weight` | `weight=N` | Load balancing weight (1-10000) |
| `ttl` | `ttl=N` | DNS TTL in seconds |
| `hc` | `hc=TYPE:PORT[:PATH]` | Health check configuration |

### Health Check Types

| Type | Format | Description |
|------|--------|-------------|
| `tcp` | `hc=tcp:PORT` | TCP connection check |
| `http` | `hc=http:PORT[:PATH]` | HTTP GET check (2xx/3xx = healthy) |
| `https` | `hc=https:PORT[:PATH]` | HTTPS GET check |
| `icmp` | `hc=icmp` | ICMP ping (requires root) |

## Storage Modes

### Single Key Mode (Default)

All hosts stored in one etcd key:

```sh
# Update hosts
cat hosts.txt | etcdctl put /etcdhosts

# Read current hosts
etcdctl get /etcdhosts
```

### Per-Host Mode

Each host stored as a separate key under the prefix:

```sh
# Set storage mode in Corefile
etcdhosts {
    key /etcdhosts/hosts/
    storage perhost
}

# Add individual hosts
etcdctl put /etcdhosts/hosts/host1 "192.168.1.1 host1.example.com"
etcdctl put /etcdhosts/hosts/host2 "192.168.1.2 host2.example.com"

# List all hosts
etcdctl get /etcdhosts/hosts/ --prefix
```

## Wildcard Matching

Wildcard entries use `*` as the first label:

```
192.168.1.1 *.example.com
192.168.1.2 *.api.example.com
192.168.1.3 specific.example.com
```

Resolution priority:
1. Exact match (`specific.example.com`)
2. Longest wildcard match (`*.api.example.com`)
3. Shorter wildcard match (`*.example.com`)

## Load Balancing

Multiple IPs for the same hostname are load balanced by weight:

```
# 75% traffic to .1, 25% to .2
192.168.1.1 api.example.com # +etcdhosts weight=3
192.168.1.2 api.example.com # +etcdhosts weight=1
```

Unhealthy backends are automatically excluded from rotation.

## Metrics

Prometheus metrics exposed at `/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `coredns_etcdhosts_queries_total` | Counter | Total queries by type and result |
| `coredns_etcdhosts_query_duration_seconds` | Histogram | Query latency |
| `coredns_etcdhosts_entries_total` | Gauge | Total host entries loaded |
| `coredns_etcdhosts_etcd_sync_total` | Counter | etcd sync operations |
| `coredns_etcdhosts_etcd_last_sync_timestamp` | Gauge | Last successful sync |
| `coredns_etcdhosts_healthcheck_status` | Gauge | Health status per target |

## Resilience

- etcd cluster failures do not crash CoreDNS - cached entries remain available
- Automatic reconnection when etcd becomes available
- Watch-based updates for real-time synchronization
- Health check failures use configurable policies

## Development

```sh
# Run tests
task test

# Run tests with coverage
task test-cover

# Run etcd integration tests
ETCD_TEST=1 go test -v ./internal/etcd/...

# Lint
task lint
```

## License

Apache 2.0
