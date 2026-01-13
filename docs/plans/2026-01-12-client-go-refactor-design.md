# client-go 重构设计

## 概述

重构 client-go 库，目标：精简、可靠、测试覆盖 80%+。同步升级 dnsctl。

## 背景

当前问题：
- 冗余代码：normalize.go (142行)、hostname.go (108行)、transport.go (51行) 未使用
- API 设计：HostsFile 与 Store 职责重叠，双重锁
- 测试覆盖：仅 45.4%

## 设计方案

### 1. 数据模型 (record.go)

保持不变，精简注释：

```go
type CheckType string

const (
    CheckTCP   CheckType = "tcp"
    CheckHTTP  CheckType = "http"
    CheckHTTPS CheckType = "https"
    CheckICMP  CheckType = "icmp"
)

type Health struct {
    Type CheckType
    Port int
    Path string
}

type Record struct {
    Hostname string
    IP       net.IP
    TTL      uint32
    Weight   int
    Health   *Health
}

type Entry struct {
    IP     net.IP
    TTL    uint32
    Weight int
    Health *Health
}
```

### 2. 主机存储 (hosts.go)

合并 HostsFile + Store 为单一类型：

```go
type Hosts struct {
    mu          sync.RWMutex
    name4       map[string][]Entry  // hostname -> IPv4
    name6       map[string][]Entry  // hostname -> IPv6
    addr        map[string][]string // IP -> hostnames
    version     int64
    modRevision int64
}

// 构造
func NewHosts() *Hosts
func Parse(data []byte) (*Hosts, error)

// 查询
func (h *Hosts) Lookup(hostname string) []Entry     // v4 + v6
func (h *Hosts) LookupV4(hostname string) []Entry
func (h *Hosts) LookupV6(hostname string) []Entry
func (h *Hosts) LookupAddr(ip string) []string

// 修改
func (h *Hosts) Add(r Record) error
func (h *Hosts) Del(hostname string, ip net.IP) error
func (h *Hosts) Purge(hostname string)

// 元数据
func (h *Hosts) Len() int
func (h *Hosts) Version() int64
func (h *Hosts) ModRevision() int64
func (h *Hosts) String() string
```

### 3. 存储模式

支持两种模式，智能自动识别：

```go
type StorageMode int

const (
    ModeSingle  StorageMode = iota  // 单 key: /etcdhosts
    ModePerHost                      // 分层: /etcdhosts/{domain}/
)
```

**元数据存储**：`{key}/.meta`

```json
{"mode": "single", "version": 1, "created": "2026-01-12T00:00:00Z"}
```

**自动识别逻辑**：

```
1. 检查 {key}/.meta
   ├─ 存在 → 读取 mode
   └─ 不存在 → 检测数据特征
       ├─ {key} 有值 → ModeSingle
       ├─ {key}/* 有子 key → ModePerHost
       └─ 都没有 → 默认 ModeSingle，创建 .meta
```

### 4. 客户端 (client.go)

```go
type Config struct {
    Endpoints   []string
    Key         string        // 默认 /etcdhosts
    DialTimeout time.Duration // 默认 5s
    ReqTimeout  time.Duration // 默认 2s

    // TLS (可选)
    TLS     *tls.Config
    CA      string  // 或文件路径/base64
    Cert    string
    CertKey string

    // 认证 (可选)
    Username string
    Password string
}

type Client struct {
    etcd    *clientv3.Client
    key     string
    timeout time.Duration
}

// 生命周期
func NewClient(cfg Config) (*Client, error)
func (c *Client) Close() error

// 读操作 (自动识别模式)
func (c *Client) Read() (*Hosts, error)
func (c *Client) ReadRevision(rev int64) (*Hosts, error)
func (c *Client) History() ([]*Hosts, error)

// 写操作 (自动识别模式)
func (c *Client) Write(h *Hosts) error
func (c *Client) ForceWrite(data []byte) error

// 模式管理
func (c *Client) Mode() (StorageMode, error)
func (c *Client) InitMode(mode StorageMode) error
```

### 5. dnsctl 重写

**命令结构**：

```
dnsctl
├── get [hostname]       # 查询记录
├── add <ip> <hostname>  # 添加记录
├── del <ip> <hostname>  # 删除记录
├── purge <hostname>     # 删除主机所有记录
├── list                 # 列出所有记录
│   ├── -o, --output     # 输出到文件
│   └── -r, --revision   # 指定 revision
├── edit                 # 交互编辑
├── restore <file>       # 从文件恢复
├── history              # 查看历史版本
│   └── -o, --output     # 导出目录
└── version              # 版本信息

全局 flags:
  -c, --config    配置文件 (默认 ~/.dnsctl.yaml)
  -k, --key       etcd key (覆盖配置)
```

**配置文件** (`~/.dnsctl.yaml`)：

```yaml
endpoints:
  - https://etcd1:2379
  - https://etcd2:2379

key: /etcdhosts

# TLS (可选)
ca: /path/to/ca.pem
cert: /path/to/cert.pem
cert_key: /path/to/key.pem

# 认证 (可选)
username: ""
password: ""

# 超时
dial_timeout: 5s
req_timeout: 2s
```

## 删除的代码

| 文件 | 行数 | 原因 |
|------|------|------|
| normalize.go | 142 | CoreDNS 遗留，未使用 |
| hostname.go | 108 | 旧设计遗留，未使用 |
| transport.go | 51 | DNS 传输常量，未使用 |

## 测试策略

**目标覆盖率**: 80%+

**测试分层**:

```
单元测试 (无外部依赖)
├── hosts_test.go      # 增删改查、序列化
├── parser_test.go     # 各种格式、边界情况
└── record_test.go     # Record/Entry 方法

集成测试 (需要 etcd)
└── client_test.go     # 读写、版本控制、模式识别
```

**重点场景**:

| 模块 | 测试场景 |
|------|----------|
| Parser | 标准 hosts、+etcdhosts 扩展、空行注释、非法格式 |
| Hosts | 添加重复、删除不存在、并发读写、IPv4/IPv6 混合 |
| Client | 单 key/分层模式、版本冲突、模式自动识别、空数据 |

## 文件结构

**client-go** (预计 ~600 行代码 + ~750 行测试):

```
client-go/
├── hosts.go          # ~200 行
├── hosts_test.go     # ~300 行
├── parser.go         # ~120 行
├── parser_test.go    # ~200 行
├── record.go         # ~50 行
├── client.go         # ~200 行
├── client_test.go    # ~250 行
├── config.go         # ~30 行
└── go.mod
```

**dnsctl** (预计 ~400 行):

```
dnsctl/
├── main.go           # 入口 + 命令定义
├── config.go         # 配置加载
└── go.mod
```

## 实施步骤

1. 重构 client-go
   - 删除未用文件
   - 合并 HostsFile + Store → Hosts
   - 简化 Parser
   - 实现智能模式识别
   - 补充单元测试

2. 重写 dnsctl
   - 基于新 client-go API
   - 实现基本命令
   - 简化配置

3. 更新 etcdhosts 插件
   - 适配新 client-go API (如有必要)

## 兼容性

- client-go 版本升级到 v3
- dnsctl 直接重写，不保持兼容
- etcdhosts 插件内部 etcd 包保持不变，仅更新 record 类型引用
