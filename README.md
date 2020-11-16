# etcdhosts

> etcdhosts 是一个 CoreDNS 插件，通过将 hosts 配置存储在 etcd 中实现分布式一致性查询。

<!--ts-->
   * [一、编译安装](#一编译安装)
      * [1.1、Docker 编译](#11docker-编译)
      * [1.2、手动编译](#12手动编译)
      * [1.3、扩展编译说明](#13扩展编译说明)
   * [二、插件配置](#二插件配置)
   * [三、数据格式](#三数据格式)
<!--te-->

## 一、编译安装

### 1.1、Docker 编译

在安装好 Docker 的 Linux 机器上，直接执行本项目下的 `build.sh` 脚本即可；编译完成后将在 build 目录下生成可执行文件压缩包。

### 1.2、手动编译

请自行 clone CoreDNS 仓库，然后修改 `plugin.cfg` 配置文件(当前 etcdhosts 基于 CoreDNS v1.6.7 开发)，并执行 `make` 既可

**`plugin.cfg` 内 etcdhosts 插入顺序影响 etcdhosts 插件执行顺序，以下配置样例中 etcdhosts 插件将优先 hosts 插件捕获 dns 请求，并根据 `fallthrough` 配置决定解析失败时是否继续穿透**

```diff
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
+etcdhosts:github.com/ytpay/etcdhosts
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
etcd:etcd
loop:loop
forward:forward
grpc:grpc
erratic:erratic
whoami:whoami
on:github.com/caddyserver/caddy/onevent
sign:sign
```

**完整编译命令如下:**

```sh
# clone source
mkdir -p ${GOPATH}/src/github.com/coredns
git clone https://github.com/coredns/coredns.git ${GOPATH}/src/github.com/coredns/coredns

# make
cd ${GOPATH}/src/github.com/coredns/coredns
git checkout tags/${VERSION} -b ${VERSION}
go get github.com/ytpay/etcdhosts@${VERSION}
sed -i '/^hosts:hosts/i\etcdhosts:github.com/ytpay/etcdhosts' plugin.cfg
make -f Makefile.release build tar DOCKER=coredns
```

编译完成后可在 release 目录下找到编译好的文件。

### 1.3、扩展编译说明

默认情况下 `build.sh` 将会挂载 `.compile.sh` 进行编译，`.compile.sh` 为真正的编译命令；默认编译时脚本
将使用 CoreDNS 版本 tag 来获取 etcdhosts 版本，但是 etcdhosts 插件版本发布可能不一定完全覆盖 CoreDNS 版本；
此时可以删除以下命令来实现永远使用最新版本的 etcdhosts:

```diff
# make
cd ${GOPATH}/src/github.com/coredns/coredns
git checkout tags/${VERSION} -b ${VERSION}
- go get github.com/ytpay/etcdhosts@${VERSION}
sed -i '/^hosts:hosts/i\etcdhosts:github.com/ytpay/etcdhosts' plugin.cfg
make -f Makefile.release build tar DOCKER=coredns
```

**需要注意的是: etcdhosts 只保证在与 CoreDNS 相匹配的 tag 版本上运行正常，不保证其他版本一定可以通过编译和运行正常。**

## 二、插件配置

etcdhosts 插件完整配置格式如下:

```sh
etcdhosts [ZONES...] {
    [INLINE]
    ttl SECONDS
    no_reverse
    fallthrough [ZONES...]
    key ETCD_KEY
    endpoint ETCD_ENDPOINT...
    credentials ETCD_USERNAME ETCD_PASSWORD
    tls ETCD_CERT ETCD_KEY ETCD_CACERT
    timeout ETCD_TIMEOUT
}
```

其中 key 默认为 `/etcdhosts`，timeout 默认为 3s，以下是一段样例配置:

```sh
etcdhosts . {
    fallthrough .
    key /etcdhosts
    timeout 5s
    tls /tmp/test_etcd_ssl/etcd.pem /tmp/test_etcd_ssl/etcd-key.pem /tmp/test_etcd_ssl/etcd-root-ca.pem
    endpoint https://172.16.11.115:2379 https://172.16.11.116:2379 https://172.16.11.117:2379
}
```

## 三、数据格式

请求到达 etcdhosts 后，etcdhosts 会向 Etcd 查询相关 key，并使用 value 作为标准的 hosts 文本进行解析；
所以如果想更新解析只需要将 hosts 文本数据写入 Etcd 既可；etcdhosts 通过 watch api 实时观测并自动重载。
