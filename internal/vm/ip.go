package vm

import (
	"fmt"
	"net"
	"sync"
)

// ipAllocator hands out static IPs from the bridge subnet, skipping the gateway.
// Plan A only; Plan B delegates addressing to the CNI plugin.
type ipAllocator struct {
	mu      sync.Mutex
	network *net.IPNet
	gateway net.IP
	used    map[string]bool
}

func newIPAllocator(cidr, gateway string) (*ipAllocator, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse subnet %q: %w", cidr, err)
	}
	gw := net.ParseIP(gateway)
	if gw == nil {
		return nil, fmt.Errorf("parse gateway %q", gateway)
	}
	return &ipAllocator{network: network, gateway: gw, used: map[string]bool{}}, nil
}

func (a *ipAllocator) next() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for ip := a.network.IP.Mask(a.network.Mask); a.network.Contains(ip); inc(ip) {
		s := ip.String()
		if ip.Equal(a.gateway) || a.used[s] || isNetworkOrBroadcast(ip, a.network) {
			continue
		}
		a.used[s] = true
		return s, nil
	}
	return "", fmt.Errorf("subnet exhausted")
}

func (a *ipAllocator) reserve(ip string) { a.mu.Lock(); a.used[ip] = true; a.mu.Unlock() }
func (a *ipAllocator) release(ip string) { a.mu.Lock(); delete(a.used, ip); a.mu.Unlock() }

func inc(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

func isNetworkOrBroadcast(ip net.IP, n *net.IPNet) bool {
	networkAddr := ip.Mask(n.Mask)
	if ip.Equal(networkAddr) {
		return true
	}
	broadcast := make(net.IP, len(networkAddr))
	for i := range networkAddr {
		broadcast[i] = networkAddr[i] | ^n.Mask[i]
	}
	return ip.Equal(broadcast)
}
