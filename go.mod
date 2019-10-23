module github.com/gozap/gdns

go 1.13

replace golang.org/x/net v0.0.0-20190813000000-74dc4d7220e7 => golang.org/x/net v0.0.0-20190827160401-ba9fcec4b297

require (
	github.com/caddyserver/caddy v1.0.3
	github.com/coredns/coredns v1.6.4
	github.com/json-iterator/go v1.1.7
	github.com/miekg/dns v1.1.22
	go.etcd.io/etcd v0.5.0-alpha.5.0.20191022185251-b5afbdd8d0e3
)
