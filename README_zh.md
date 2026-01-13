# etcdhosts

> etcdhosts 是一个 CoreDNS 插件, 将 hosts 配置存储在 etcd 中, 实现集中管理和多节点一致性.

## 功能特性

**v2.0** 完全重写, 新增以下功能:

- **通配符支持** - 匹配 `*.example.com` 模式, 基于优先级解析
- **加权负载均衡** - 按可配置权重在多个后端间分发流量
- **健康检查** - 支持 TCP、HTTP、HTTPS、ICMP 探针, 可配置阈值
- **扩展 Hosts 格式** - 通过 `# +etcdhosts` 内联配置 TTL、权重和健康检查
- **存储模式** - 单键模式 (默认) 或按主机分键存储
- **Prometheus 指标** - 查询计数、延迟、健康状态和同步指标
- **IPv4/IPv6 支持** - 完整双栈支持, 含反向 DNS 查询

## 环境要求

- Go 1.25+
- CoreDNS v1.13.2+
- etcd v3.5+
- [go-task](https://taskfile.dev/installation/)
- GNU sed (macOS 用户: `brew install gnu-sed`)

## 安装

```sh
# 克隆并构建
git clone https://github.com/etcdhosts/etcdhosts.git
cd etcdhosts
task all

# 二进制文件在 dist/ 目录
ls dist/
```

## 配置

### 基本配置

```
etcdhosts [ZONES...] {
    endpoint ETCD_ENDPOINT...
    key ETCD_KEY
    [fallthrough [ZONES...]]
}
```

### 完整配置

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

### 配置选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `endpoint` | (必填) | etcd 端点, 空格分隔 |
| `key` | `/etcdhosts` | etcd 键或前缀 |
| `storage` | `single` | 存储模式: `single` (单键) 或 `perhost` (按主机分键) |
| `ttl` | `3600` | 默认 DNS TTL (秒) |
| `timeout` | `5s` | etcd 连接超时 |
| `credentials` | - | etcd 用户名和密码 |
| `tls` | - | TLS 证书、密钥和 CA 文件 |
| `fallthrough` | - | 无匹配时传递给下一个插件 |

### 健康检查选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `interval` | `10s` | 检查间隔 |
| `timeout` | `3s` | 检查超时 |
| `max_concurrent` | `10` | 最大并发检查数 |
| `unhealthy_policy` | `return_all` | 所有后端不健康时的策略 |

### Corefile 示例

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

## 数据格式

### 基本格式

标准 `/etc/hosts` 格式:

```
192.168.1.1 host1.example.com
192.168.1.2 host2.example.com host2
2001:db8::1 ipv6host.example.com
```

### 扩展格式

使用 `# +etcdhosts` 标记启用高级功能:

```
# 基本条目
192.168.1.1 host1.example.com

# 带权重和 TTL
192.168.1.2 host2.example.com # +etcdhosts weight=10 ttl=300

# TCP 健康检查
192.168.1.3 db.example.com # +etcdhosts hc=tcp:3306

# HTTP 健康检查
192.168.1.4 api.example.com # +etcdhosts weight=5 hc=http:8080:/health

# HTTPS 健康检查
192.168.1.5 secure.example.com # +etcdhosts hc=https:443:/healthz

# ICMP 健康检查 (需要 root 权限)
192.168.1.6 server.example.com # +etcdhosts hc=icmp

# 通配符条目
192.168.1.10 *.apps.example.com # +etcdhosts weight=1

# 完整示例
192.168.1.100 prod.example.com # +etcdhosts weight=100 ttl=60 hc=http:80:/status
```

### 扩展格式选项

| 选项 | 格式 | 说明 |
|------|------|------|
| `weight` | `weight=N` | 负载均衡权重 (1-10000) |
| `ttl` | `ttl=N` | DNS TTL (秒) |
| `hc` | `hc=TYPE:PORT[:PATH]` | 健康检查配置 |

### 健康检查类型

| 类型 | 格式 | 说明 |
|------|------|------|
| `tcp` | `hc=tcp:PORT` | TCP 连接检查 |
| `http` | `hc=http:PORT[:PATH]` | HTTP GET 检查 (2xx/3xx = 健康) |
| `https` | `hc=https:PORT[:PATH]` | HTTPS GET 检查 |
| `icmp` | `hc=icmp` | ICMP ping (需要 root) |

## 存储模式

### 单键模式 (默认)

所有主机存储在一个 etcd 键中:

```sh
# 更新 hosts
cat hosts.txt | etcdctl put /etcdhosts

# 读取当前 hosts
etcdctl get /etcdhosts
```

### 按主机分键模式

每个主机作为前缀下的独立键存储:

```sh
# 在 Corefile 中设置存储模式
etcdhosts {
    key /etcdhosts/hosts/
    storage perhost
}

# 添加单个主机
etcdctl put /etcdhosts/hosts/host1 "192.168.1.1 host1.example.com"
etcdctl put /etcdhosts/hosts/host2 "192.168.1.2 host2.example.com"

# 列出所有主机
etcdctl get /etcdhosts/hosts/ --prefix
```

## 通配符匹配

通配符条目使用 `*` 作为第一个标签:

```
192.168.1.1 *.example.com
192.168.1.2 *.api.example.com
192.168.1.3 specific.example.com
```

解析优先级:
1. 精确匹配 (`specific.example.com`)
2. 最长通配符匹配 (`*.api.example.com`)
3. 较短通配符匹配 (`*.example.com`)

## 负载均衡

同一主机名的多个 IP 按权重进行负载均衡:

```
# 75% 流量到 .1, 25% 到 .2
192.168.1.1 api.example.com # +etcdhosts weight=3
192.168.1.2 api.example.com # +etcdhosts weight=1
```

不健康的后端会自动从轮询中排除.

## 指标

Prometheus 指标暴露在 `/metrics`:

| 指标 | 类型 | 说明 |
|------|------|------|
| `coredns_etcdhosts_queries_total` | Counter | 按类型和结果统计的总查询数 |
| `coredns_etcdhosts_query_duration_seconds` | Histogram | 查询延迟 |
| `coredns_etcdhosts_entries_total` | Gauge | 已加载的主机条目总数 |
| `coredns_etcdhosts_etcd_sync_total` | Counter | etcd 同步操作数 |
| `coredns_etcdhosts_etcd_last_sync_timestamp` | Gauge | 最后成功同步时间 |
| `coredns_etcdhosts_healthcheck_status` | Gauge | 每个目标的健康状态 |

## 容错性

- etcd 集群故障不会导致 CoreDNS 崩溃 - 缓存条目保持可用
- etcd 恢复后自动重连
- 基于 Watch 的更新实现实时同步
- 健康检查失败使用可配置策略

## 开发

```sh
# 运行测试
task test

# 运行测试并生成覆盖率
task test-cover

# 运行 etcd 集成测试
ETCD_TEST=1 go test -v ./internal/etcd/...

# 代码检查
task lint
```

## 相关项目

- [client-go](https://github.com/etcdhosts/client-go) - Go 客户端库
- [dnsctl](https://github.com/etcdhosts/dnsctl) - 命令行管理工具

## 许可证

Apache 2.0
