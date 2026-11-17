// Package discovery implements conservative LAN discovery: ARP-table
// inspection, ICMP ping sweeps over a bounded CIDR and a single TCP port
// probe to recognise devices running an agent. It never attempts logins or
// any form of unauthorized access.
package discovery

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Device is a discovered host.
type Device struct {
	IP        string
	MAC       string
	Hostname  string
	OpenPorts string
	Responded bool
}

type Scanner struct {
	// MaxHosts caps the number of addresses a single scan may touch.
	MaxHosts int
	// AgentPort is the server port agents would be reachable on (probed for
	// identification only).
	AgentPort int
	// AgentPath is the HTTP path probed to identify an agent endpoint.
	AgentPath string
}

func New(agentPort int) *Scanner {
	return &Scanner{MaxHosts: 1024, AgentPort: agentPort, AgentPath: "/api/agent/ping"}
}

// ParseTargets turns an IP or CIDR into a list of addresses (bounded).
func (s *Scanner) ParseTargets(target string) ([]string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("target is required")
	}
	if !strings.Contains(target, "/") {
		if net.ParseIP(target) == nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR range", target)
		}
		return []string{target}, nil
	}
	ip, ipnet, err := net.ParseCIDR(target)
	if err != nil {
		return nil, err
	}
	var ips []string
	for cur := ip.Mask(ipnet.Mask); ipnet.Contains(cur); inc(cur) {
		ips = append(ips, cur.String())
		if len(ips) > s.MaxHosts {
			return nil, fmt.Errorf("range too large: limit is %d addresses", s.MaxHosts)
		}
	}
	// Skip network and broadcast addresses for IPv4 ranges larger than /31.
	if ones, bits := ipnet.Mask.Size(); bits == 32 && ones <= 30 {
		ips = ips[1 : len(ips)-1]
	}
	return ips, nil
}

func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// Scan pings each address, probes the agent port, and merges ARP entries.
func (s *Scanner) Scan(ctx context.Context, ips []string, progress func(scanned, found int)) ([]Device, error) {
	results := make([]Device, len(ips))
	sem := make(chan struct{}, 48)
	var wg sync.WaitGroup
	var mu sync.Mutex
	found := 0
	scanned := 0
	for i, ip := range ips {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d := s.probe(ctx, ip)
			results[i] = d
			mu.Lock()
			scanned++
			if d.Responded || d.MAC != "" {
				found++
			}
			if progress != nil && scanned%16 == 0 {
				progress(scanned, found)
			}
			mu.Unlock()
		}(i, ip)
	}
	wg.Wait()
	if progress != nil {
		progress(scanned, found)
	}
	var out []Device
	for _, d := range results {
		if d.Responded || d.MAC != "" {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return ipLess(out[i].IP, out[j].IP) })
	// Enrich MACs from the ARP table (entries appear after the pings).
	arp := s.ARPTable(ctx)
	for i := range out {
		if out[i].MAC == "" {
			out[i].MAC = arp[out[i].IP]
		}
	}
	return out, nil
}

func ipLess(a, b string) bool {
	ia := net.ParseIP(a).To4()
	ib := net.ParseIP(b).To4()
	if ia == nil || ib == nil {
		return a < b
	}
	return binary.BigEndian.Uint32(ia) < binary.BigEndian.Uint32(ib)
}

func (s *Scanner) probe(ctx context.Context, ip string) Device {
	d := Device{IP: ip}
	// 1. ICMP ping (single echo, short timeout).
	if s.ping(ctx, ip) {
		d.Responded = true
	}
	// 2. TCP probe of the agent API port.
	if s.AgentPort > 0 {
		addr := net.JoinHostPort(ip, strconv.Itoa(s.AgentPort))
		conn, err := net.DialTimeout("tcp", addr, 700*time.Millisecond)
		if err == nil {
			conn.Close()
			d.Responded = true
			d.OpenPorts = strconv.Itoa(s.AgentPort)
		}
	}
	// 3. Reverse DNS for a hostname hint.
	if d.Responded {
		rctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		defer cancel()
		if names, err := net.DefaultResolver.LookupAddr(rctx, ip); err == nil && len(names) > 0 {
			d.Hostname = strings.TrimSuffix(names[0], ".")
		}
	}
	return d
}

func (s *Scanner) ping(ctx context.Context, ip string) bool {
	cctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(cctx, "ping", "-n", "1", "-w", "800", ip)
	default:
		cmd = exec.CommandContext(cctx, "ping", "-c", "1", "-W", "1", ip)
	}
	return cmd.Run() == nil
}

var winARP = regexp.MustCompile(`(?m)^\s*(\d+\.\d+\.\d+\.\d+)\s+([0-9a-fA-F-]{11,17})\s+\w+`)

// ARPTable returns IP -> MAC from the OS ARP cache.
func (s *Scanner) ARPTable(ctx context.Context) map[string]string {
	out := map[string]string{}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cctx, "arp", "-a")
	} else {
		cmd = exec.CommandContext(cctx, "arp", "-an")
	}
	b, err := cmd.Output()
	if err != nil {
		return out
	}
	text := string(b)
	if runtime.GOOS == "windows" {
		for _, m := range winARP.FindAllStringSubmatch(text, -1) {
			mac := strings.ToUpper(strings.ReplaceAll(m[2], "-", ":"))
			if mac != "FF:FF:FF:FF:FF:FF" && !strings.HasPrefix(mac, "01:00:5E") {
				out[m[1]] = mac
			}
		}
		return out
	}
	// BSD/Linux: "? (192.168.1.5) at aa:bb:cc:dd:ee:ff on en0 ..."
	re := regexp.MustCompile(`\((\d+\.\d+\.\d+\.\d+)\) at ([0-9a-fA-F:]{17})`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out[m[1]] = strings.ToUpper(m[2])
	}
	return out
}

// LocalSubnet returns the CIDR of the first non-loopback IPv4 interface.
func LocalSubnet() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				if v4 := ipnet.IP.To4(); v4 != nil {
					ones, _ := ipnet.Mask.Size()
					if ones >= 22 && ones <= 30 {
						return (&net.IPNet{IP: v4.Mask(ipnet.Mask), Mask: ipnet.Mask}).String()
					}
				}
			}
		}
	}
	return ""
}
