package etcdhosts

import "github.com/coredns/coredns/plugin"

const pluginName = "etcdhosts"

func init() {
	plugin.Register(pluginName, setup)
}
