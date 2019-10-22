# gdns

> gdns 是一个 CoreDNS 插件，后端对接 Etcd，以实现分布一致性的 CoreDNS 集群；插件目前只实现了相关记录的精确查找，部分记录类型尚未实现

## 编译安装

请自行 clone CoreDNS 仓库，然后修改 `plugin.cfg` 配置文件(当前 gdns 基于 CoreDNS v1.6.4 开发)，并执行 `make` 既可

**`plugin.cfg` 内 gdns 插入顺序影响 gdns 插件执行顺序，以下配置样例中 gdns 插件将优先 etcd 插件捕获 dns 请求，并根据 `fallthrough` 配置决定解析失败时是否继续穿透**

```shell script
# Directives are registered in the order they should be
# executed.
#
# Ordering is VERY important. Every plugin will
# feel the effects of all other plugin below
# (after) them during a request, but they must not
# care what plugin above them are doing.

# How to rebuild with updated plugin configurations:
# Modify the list below and run `go gen && go build`

# The parser takes the input format of
#     <plugin-name>:<package-name>
# Or
#     <plugin-name>:<fully-qualified-package-name>
#
# External plugin example:
# log:github.com/coredns/coredns/plugin/log
# Local plugin example:
# log:log

metadata:metadata
cancel:cancel
tls:tls
reload:reload
nsid:nsid
root:root
bind:bind
debug:debug
trace:trace
ready:ready
health:health
pprof:pprof
prometheus:metrics
errors:errors
log:log
dnstap:dnstap
acl:acl
any:any
chaos:chaos
loadbalance:loadbalance
cache:cache
rewrite:rewrite
dnssec:dnssec
autopath:autopath
template:template
hosts:hosts
route53:route53
azure:azure
clouddns:clouddns
federation:github.com/coredns/federation
k8s_external:k8s_external
kubernetes:kubernetes
file:file
auto:auto
secondary:secondary
gdns:github.com/gozap/gdns
etcd:etcd
loop:loop
forward:forward
grpc:grpc
erratic:erratic
whoami:whoami
on:github.com/caddyserver/caddy/onevent
sign:sign
```

## 插件配置

gdns 插件配置与 [etcd](https://coredns.io/plugins/etcd/) 插件配置完全相同，并且移除了 etcd 内无效配置(已经无效，但允许写入的字段)

## 数据格式

请求到达 gdns 后，gdns 会向 Etcd 查询相关 key，并使用 value 反序列化后得到结果

**key 格式: `GDNS_PATHPREFIX + / + DOMAIN_NAME`**

- GDNS_PATHPREFIX: 默认为 `/gdns`
- DOMAIN_NAME: 域名, 例如 `test.com`

Example: `/gdns/test.com`

**value 格式: `'{"QType":[{"domain":"DOMAIN","sub_domain":"SUB_DOMAIN","type":QTYPE,"record":"RECORD","ttl":TTL}]}'`**

- QType: 查询类型(uint16), 取值参考 [miekg/dns](https://github.com/miekg/dns/blob/40eab7a196d1397aa407c5c9b726fc48b1a9e9e8/types.go#L26)
- domain: 基础域名(string)，例如 `example.com`
- sub_domain: 子域名(string)，例如 `test`
- record: 具体记录(string), 字符串内容根据实际 DNS 请求确定，例如 NS 请求字符串必须为 FQDN
- ttl: Time to live(uint32)

Example: `{"1":[{"domain":"example.com","sub_domain":"test","type":1,"record":"1.2.3.4","ttl":600}],"28":[{"domain":"example.com","sub_domain":"ipv6","type":28,"record":"2001:0db8:3c4d:0015:0000:0000:1a2f:1a2b","ttl":600}]}`

**etcdctl 命令样例**

```shell script
etcdctl put /gdns/example.com '{"1":[{"domain":"example.com","sub_domain":"test","type":1,"record":"1.2.3.4","ttl":600}],"28":[{"domain":"example.com","sub_domain":"ipv6","type":28,"record":"2001:0db8:3c4d:0015:0000:0000:1a2f:1a2b","ttl":600}]}'
```