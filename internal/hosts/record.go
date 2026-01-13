package hosts

import (
	client "github.com/etcdhosts/client-go/v2"
)

// Re-export types from client-go for backward compatibility.
type (
	Record    = client.Record
	Health    = client.Health
	CheckType = client.CheckType
	Entry     = client.Entry
)

// Re-export constants from client-go.
const (
	CheckTCP   = client.CheckTCP
	CheckHTTP  = client.CheckHTTP
	CheckHTTPS = client.CheckHTTPS
	CheckICMP  = client.CheckICMP
)

// ParseRecords parses hosts data and returns a slice of Records.
// This is a wrapper around client.ParseRecords for convenience.
func ParseRecords(data []byte) ([]Record, error) {
	return client.ParseRecords(data)
}
