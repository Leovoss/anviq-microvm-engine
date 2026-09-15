package vm

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"
)

// dialGuest opens a host-initiated connection to a service inside the microVM
// over Firecracker's vsock, using Firecracker's documented handshake:
//
//	1. connect to the per-VM Unix domain socket Firecracker created (box.vsockUDS),
//	2. send "CONNECT <guest_port>\n",
//	3. read "OK <host_port>\n",
//	4. stream raw bytes to/from the guest's AF_VSOCK listener on that port.
//
// This is pure stdlib on the host side — only the guest agent needs AF_VSOCK.
func dialGuest(udsPath string, port uint32, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", udsPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial vsock uds %s: %w", udsPath, err)
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock CONNECT: %w", err)
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake read: %w", err)
	}
	if !strings.HasPrefix(line, "OK ") {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake refused: %q", strings.TrimSpace(line))
	}
	// Clear the deadline; the caller manages further timeouts via context.
	_ = conn.SetDeadline(time.Time{})
	// Hand back a conn whose reads drain the buffered reader first (the handshake
	// may have buffered guest bytes that follow the OK line).
	return &bufferedConn{Conn: conn, r: br}, nil
}

// bufferedConn is a net.Conn whose Read drains a bufio.Reader before the socket,
// so no bytes buffered during the handshake are lost.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
