# etcdhosts

> etcdhosts 是一个 CoreDNS 插件, 通过将 hosts 配置存储在 etcd 中实现 hosts 配置统一管理和多节点一致性.

<!--ts-->
   * [一、编译安装](#一编译安装)
   * [二、插件配置](#二插件配置)
   * [三、数据格式](#三数据格式)
<!--te-->

## 一、编译安装

编译本项目前请确保安装以下工具:

- Git
- Go 1.9+
- [go-task](https://taskfile.dev/installation/)
- GNU sed(MacOS 用户需要手动安装 `brew install gnu-sed`)

安装完成后在本项目目录下执行 `task` 命令即可自动完成编译:

```sh
~/g/s/g/e/etcdhosts ❯❯❯ task                                                                                                                                                       master ✱
task: [clean] rm -rf coredns dist
task: [clone-source] mkdir dist
task: [clone-source] git clone --depth 1 --branch v1.10.0 https://github.com/coredns/coredns.git coredns
正克隆到 'coredns'...
remote: Enumerating objects: 918, done.
remote: Counting objects: 100% (918/918), done.
remote: Compressing objects: 100% (856/856), done.
remote: Total 918 (delta 75), reused 272 (delta 47), pack-reused 0
接收对象中: 100% (918/918), 839.03 KiB | 981.00 KiB/s, 完成.
处理 delta 中: 100% (75/75), 完成.
注意：正在切换到 '596a9f9e67dd9b01e15bc04a999460422fe65166'。

...

task: [build] mv release/* ../dist
```

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
    force_reload FORCE_RELOAD_INTERVAL
}
```

其中 key 默认为 `/etcdhosts`, timeout 默认为 `3s`, 以下是一段样例配置:

```sh
etcdhosts . {
    fallthrough .
    key /etcdhosts
    timeout 5s
    tls /tmp/test_etcd_ssl/etcd.pem /tmp/test_etcd_ssl/etcd-key.pem /tmp/test_etcd_ssl/etcd-root-ca.pem
    endpoint https://172.16.11.115:2379 https://172.16.11.116:2379 https://172.16.11.117:2379
}
```

**默认情况下, 即使 Etcd 集群故障也可以启动成功, 插件会在后台自动重连. 同样如果 CoreDNS 启动后 Etcd 集群失联也不会导致解析丢失,
插件也会自动重连;** 为了保证一些极端情况下依然可靠, 从 `v1.10.0` 版本开始增加了 `force_reload` 配置, 当设置后插件将会在指定间隔时间
强制读取 Etcd 数据进行刷新(读取失败不会删除缓存的 DNS 记录).

## 三、数据格式

CoreDNS 启动后 etcdhosts 会向 Etcd 查询指定的 key, 并使用 value 作为标准的 hosts 文本进行解析;
如果想更新解析只需要将 hosts 文本数据写入 Etcd 指定的 key 既可; 同时 etcdhosts 也会通过 watch api 实时观测并自动重载.

如果想要扩展开发只需要对接标准的 Etcd API 即可, 同样你也可以通过标准的 `etcdctl` 来更新 hosts 文件:

```sh
# 通过 etcdctl 更新 hosts
cat hosts | etcdctl put /etcdhosts
```
