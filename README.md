# etcdhosts

> etcdhosts 是一个 CoreDNS 插件，通过将 hosts 配置存储在 etcd 中实现 hosts 配置统一管理和多节点一致性。

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
    force_start BOOLEAN
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
