module github.com/gozap/gdns

go 1.13

replace (
	github.com/coreos/etcd v3.3.17+incompatible => go.etcd.io/etcd v3.3.17+incompatible
	golang.org/x/net v0.0.0-20190813000000-74dc4d7220e7 => golang.org/x/net v0.0.0-20191007182048-72f939374954
)

require (
	github.com/caddyserver/caddy v1.0.3
	github.com/coredns/coredns v1.6.4
	github.com/json-iterator/go v1.1.7
	github.com/miekg/dns v1.1.22
	go.etcd.io/etcd v0.0.0-20190823073701-67d0c21bb04c
)
