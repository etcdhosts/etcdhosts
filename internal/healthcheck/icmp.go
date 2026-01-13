package healthcheck

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// ICMPProbe implements the Probe interface using ICMP echo (ping) checks.
// It sends ICMP echo requests to verify that the target is reachable at the network layer.
// Note: ICMP probes typically require root/administrator privileges.
type ICMPProbe struct {
	timeout time.Duration
}

// NewICMPProbe creates a new ICMPProbe with the specified timeout.
// The timeout is used as the maximum duration for waiting for an echo reply.
func NewICMPProbe(timeout time.Duration) *ICMPProbe {
	return &ICMPProbe{
		timeout: timeout,
	}
}

// Check sends an ICMP echo request to the specified IP address and waits for a reply.
// The port and path parameters are ignored for ICMP probes.
// Returns nil if an echo reply is received, or an error if the check fails.
func (p *ICMPProbe) Check(ctx context.Context, ip net.IP, _ int, _ string) error {
	// Determine IP version and set appropriate parameters
	isIPv6 := ip.To4() == nil

	var network string
	var proto int
	var msgType icmp.Type

	if isIPv6 {
		network = "ip6:ipv6-icmp"
		proto = 58 // ICMPv6
		msgType = ipv6.ICMPTypeEchoRequest
	} else {
		network = "ip4:icmp"
		proto = 1 // ICMP
		msgType = ipv4.ICMPTypeEcho
	}

	// Listen for ICMP packets
	conn, err := icmp.ListenPacket(network, "")
	if err != nil {
		return fmt.Errorf("icmp listen failed: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Build ICMP echo message
	msg := icmp.Message{
		Type: msgType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("etcdhosts-healthcheck"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return fmt.Errorf("icmp marshal failed: %w", err)
	}

	// Set deadline based on context and probe timeout
	deadline := time.Now().Add(p.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("icmp set deadline failed: %w", err)
	}

	// Send ICMP echo request
	dst := &net.IPAddr{IP: ip}
	if _, err := conn.WriteTo(msgBytes, dst); err != nil {
		return fmt.Errorf("icmp write failed: %w", err)
	}

	// Wait for reply
	reply := make([]byte, 1500)
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("icmp check cancelled: %w", ctx.Err())
		default:
		}

		n, peer, err := conn.ReadFrom(reply)
		if err != nil {
			return fmt.Errorf("icmp read failed: %w", err)
		}

		// Parse the reply
		rm, err := icmp.ParseMessage(proto, reply[:n])
		if err != nil {
			// Continue waiting for a valid reply
			continue
		}

		// Check if it's an echo reply from the target
		var isEchoReply bool
		if isIPv6 {
			isEchoReply = rm.Type == ipv6.ICMPTypeEchoReply
		} else {
			isEchoReply = rm.Type == ipv4.ICMPTypeEchoReply
		}

		if !isEchoReply {
			// Not an echo reply, continue waiting
			continue
		}

		// Verify it's from the correct peer
		peerIP := peer.(*net.IPAddr).IP
		if !peerIP.Equal(ip) {
			// Reply from different host, continue waiting
			continue
		}

		// Verify echo ID matches
		echoReply, ok := rm.Body.(*icmp.Echo)
		if !ok {
			continue
		}
		if echoReply.ID != os.Getpid()&0xffff {
			// Reply for different process, continue waiting
			continue
		}

		// Successfully received echo reply
		return nil
	}
}
