// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file is a modified version of net/hosts.go from the golang repo

package hosts

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"go.etcd.io/etcd/clientv3"

	"github.com/coredns/coredns/plugin"
)

// parseIP calls discards any v6 zone info, before calling net.ParseIP.
func parseIP(addr string) net.IP {
	if i := strings.Index(addr, "%"); i >= 0 {
		// discard ipv6 zone
		addr = addr[0:i]
	}

	return net.ParseIP(addr)
}

type options struct {
	// automatically generate IP to Hostname PTR entries
	// for host entries we parse
	autoReverse bool

	// The TTL of the record we generate
	ttl uint32
}

func newOptions() *options {
	return &options{
		autoReverse: true,
		ttl:         3600,
	}
}

// Map contains the IPv4/IPv6 and reverse mapping.
type Map struct {
	// Key for the list of literal IP addresses must be a FQDN lowercased host name.
	name4 map[string][]net.IP
	name6 map[string][]net.IP

	// Key for the list of host names must be a literal IP address
	// including IPv6 address without zone identifier.
	// We don't support old-classful IP address notation.
	addr map[string][]string
}

func newMap() *Map {
	return &Map{
		name4: make(map[string][]net.IP),
		name6: make(map[string][]net.IP),
		addr:  make(map[string][]string),
	}
}

// Len returns the total number of addresses in the hostmap, this includes V4/V6 and any reverse addresses.
func (h *Map) Len() int {
	l := 0
	for _, v4 := range h.name4 {
		l += len(v4)
	}
	for _, v6 := range h.name6 {
		l += len(v6)
	}
	for _, a := range h.addr {
		l += len(a)
	}
	return l
}

// Hostsfile contains known host entries.
type Hostsfile struct {
	sync.RWMutex

	// list of zones we are authoritative for
	Origins []string

	// hosts maps for lookups
	hmap *Map

	// inline saves the hosts file that is inlined in a Corefile.
	inline *Map

	// etcd tls config
	etcdTLSConfig *tls.Config

	// etcd user and passwd
	etcdUserName string
	etcdPassword string

	// etcd endpoints
	etcdEndpoints []string

	// etcd v3 client
	etcdClient *clientv3.Client

	// etcd client timeout
	etcdTimeout time.Duration

	// etcd key
	etcdHostsKey string

	// etcdKeyVersion are only read and modified by a single goroutine
	etcdKeyVersion int64

	options *options
}

// readHosts determines if the cached data needs to be updated based on the size and modification time of the hostsfile.
func (h *Hostsfile) readHosts() {

	ctx, cancel := context.WithTimeout(context.Background(), h.etcdTimeout)
	defer cancel()
	getResp, err := h.etcdClient.Get(ctx, h.etcdHostsKey)
	if err != nil {
		log.Errorf("failed to get etcd key [%s]: %s", h.etcdHostsKey, err.Error())
		return
	}

	if len(getResp.Kvs) != 1 {
		log.Errorf("invalid etcd response: %d", len(getResp.Kvs))
		return
	}

	h.RLock()
	version := h.etcdKeyVersion
	h.RUnlock()

	// if version not changed, skip reading
	if version == getResp.Kvs[0].Version {
		return
	}

	newMap := h.parse(bytes.NewReader(getResp.Kvs[0].Value))
	log.Debugf("Parsed hosts file into %d entries", newMap.Len())

	h.Lock()
	h.hmap = newMap
	// Update the data cache.
	h.etcdKeyVersion = getResp.Kvs[0].Version
	hostsEntries.WithLabelValues().Set(float64(h.inline.Len() + h.hmap.Len()))
	h.Unlock()
}

func (h *Hostsfile) initInline(inline []string) {
	if len(inline) == 0 {
		return
	}

	h.inline = h.parse(strings.NewReader(strings.Join(inline, "\n")))
}

// Parse reads the hostsfile and populates the byName and addr maps.
func (h *Hostsfile) parse(r io.Reader) *Map {
	hmap := newMap()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if i := bytes.Index(line, []byte{'#'}); i >= 0 {
			// Discard comments.
			line = line[0:i]
		}
		f := bytes.Fields(line)
		if len(f) < 2 {
			continue
		}
		addr := parseIP(string(f[0]))
		if addr == nil {
			continue
		}

		family := 0
		if addr.To4() != nil {
			family = 1
		} else {
			family = 2
		}

		for i := 1; i < len(f); i++ {
			name := plugin.Name(string(f[i])).Normalize()
			if plugin.Zones(h.Origins).Matches(name) == "" {
				// name is not in Origins
				continue
			}
			switch family {
			case 1:
				hmap.name4[name] = append(hmap.name4[name], addr)
			case 2:
				hmap.name6[name] = append(hmap.name6[name], addr)
			default:
				continue
			}
			if !h.options.autoReverse {
				continue
			}
			hmap.addr[addr.String()] = append(hmap.addr[addr.String()], name)
		}
	}

	return hmap
}

// lookupStaticHost looks up the IP addresses for the given host from the hosts file.
func (h *Hostsfile) lookupStaticHost(m map[string][]net.IP, host string) []net.IP {
	h.RLock()
	defer h.RUnlock()

	if len(m) == 0 {
		return nil
	}

	ips, ok := m[host]
	if !ok {
		return nil
	}
	ipsCp := make([]net.IP, len(ips))
	copy(ipsCp, ips)
	return ipsCp
}

// LookupStaticHostV4 looks up the IPv4 addresses for the given host from the hosts file.
func (h *Hostsfile) LookupStaticHostV4(host string) []net.IP {
	host = strings.ToLower(host)
	ip1 := h.lookupStaticHost(h.hmap.name4, host)
	ip2 := h.lookupStaticHost(h.inline.name4, host)
	return append(ip1, ip2...)
}

// LookupStaticHostV6 looks up the IPv6 addresses for the given host from the hosts file.
func (h *Hostsfile) LookupStaticHostV6(host string) []net.IP {
	host = strings.ToLower(host)
	ip1 := h.lookupStaticHost(h.hmap.name6, host)
	ip2 := h.lookupStaticHost(h.inline.name6, host)
	return append(ip1, ip2...)
}

// LookupStaticAddr looks up the hosts for the given address from the hosts file.
func (h *Hostsfile) LookupStaticAddr(addr string) []string {
	addr = parseIP(addr).String()
	if addr == "" {
		return nil
	}

	h.RLock()
	defer h.RUnlock()
	hosts1 := h.hmap.addr[addr]
	hosts2 := h.inline.addr[addr]

	if len(hosts1) == 0 && len(hosts2) == 0 {
		return nil
	}

	hostsCp := make([]string, len(hosts1)+len(hosts2))
	copy(hostsCp, hosts1)
	copy(hostsCp[len(hosts1):], hosts2)
	return hostsCp
}
