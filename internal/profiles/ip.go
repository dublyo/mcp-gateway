package profiles

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"net"
	"strings"
)

type IpProfile struct{}

func (p *IpProfile) ID() string { return "ip" }

func (p *IpProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "ip_info",
			Description: "Get information about an IP address (version, type, class, reverse DNS)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"ip": map[string]interface{}{"type": "string", "description": "IP address to analyze"},
				},
				"required": []string{"ip"},
			},
		},
		{
			Name:        "cidr_info",
			Description: "Get information about a CIDR range (network, broadcast, host range, host count)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"cidr": map[string]interface{}{"type": "string", "description": "CIDR notation (e.g. 192.168.1.0/24)"},
				},
				"required": []string{"cidr"},
			},
		},
		{
			Name:        "ip_in_range",
			Description: "Check if an IP address is within a CIDR range",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"ip":   map[string]interface{}{"type": "string", "description": "IP address to check"},
					"cidr": map[string]interface{}{"type": "string", "description": "CIDR range"},
				},
				"required": []string{"ip", "cidr"},
			},
		},
		{
			Name:        "subnet_calculator",
			Description: "Calculate subnets for a network with a given number of hosts or subnets",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"network":      map[string]interface{}{"type": "string", "description": "Base network in CIDR notation (e.g. 10.0.0.0/16)"},
					"hosts_needed": map[string]interface{}{"type": "integer", "description": "Number of hosts needed per subnet"},
				},
				"required": []string{"network", "hosts_needed"},
			},
		},
	}
}

func (p *IpProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "ip_info":
		return p.ipInfo(args)
	case "cidr_info":
		return p.cidrInfo(args)
	case "ip_in_range":
		return p.ipInRange(args)
	case "subnet_calculator":
		return p.subnetCalculator(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *IpProfile) ipInfo(args map[string]interface{}) (string, error) {
	ipStr := getStr(args, "ip")
	if ipStr == "" {
		return "", fmt.Errorf("ip is required")
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", fmt.Errorf("invalid IP address: %s", ipStr)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("IP Address: %s", ip.String()))

	if ip.To4() != nil {
		lines = append(lines, "Version: IPv4")
		// Class
		first := ip.To4()[0]
		switch {
		case first < 128:
			lines = append(lines, "Class: A (1.0.0.0 - 126.255.255.255)")
		case first < 192:
			lines = append(lines, "Class: B (128.0.0.0 - 191.255.255.255)")
		case first < 224:
			lines = append(lines, "Class: C (192.0.0.0 - 223.255.255.255)")
		case first < 240:
			lines = append(lines, "Class: D - Multicast (224.0.0.0 - 239.255.255.255)")
		default:
			lines = append(lines, "Class: E - Reserved (240.0.0.0 - 255.255.255.255)")
		}
	} else {
		lines = append(lines, "Version: IPv6")
	}

	// Type checks
	var types []string
	if ip.IsLoopback() {
		types = append(types, "Loopback")
	}
	if ip.IsPrivate() {
		types = append(types, "Private")
	}
	if ip.IsGlobalUnicast() {
		types = append(types, "Global Unicast")
	}
	if ip.IsLinkLocalUnicast() {
		types = append(types, "Link-Local Unicast")
	}
	if ip.IsLinkLocalMulticast() {
		types = append(types, "Link-Local Multicast")
	}
	if ip.IsMulticast() {
		types = append(types, "Multicast")
	}
	if ip.IsUnspecified() {
		types = append(types, "Unspecified")
	}
	lines = append(lines, fmt.Sprintf("Type: %s", strings.Join(types, ", ")))

	// Reverse DNS
	names, err := net.LookupAddr(ipStr)
	if err == nil && len(names) > 0 {
		lines = append(lines, fmt.Sprintf("Reverse DNS: %s", strings.Join(names, ", ")))
	} else {
		lines = append(lines, "Reverse DNS: none")
	}

	return strings.Join(lines, "\n"), nil
}

func (p *IpProfile) cidrInfo(args map[string]interface{}) (string, error) {
	cidr := getStr(args, "cidr")
	if cidr == "" {
		return "", fmt.Errorf("cidr is required")
	}
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR: %s", err)
	}

	ones, totalBits := ipNet.Mask.Size()
	hostBits := totalBits - ones
	totalHosts := int64(math.Pow(2, float64(hostBits)))
	usableHosts := totalHosts - 2
	if usableHosts < 0 {
		usableHosts = 0
	}

	network := ipNet.IP
	var broadcast net.IP
	if ip.To4() != nil {
		b := make(net.IP, 4)
		n := binary.BigEndian.Uint32(network.To4())
		mask := binary.BigEndian.Uint32(net.IP(ipNet.Mask).To4())
		binary.BigEndian.PutUint32(b, n|^mask)
		broadcast = b

		// First usable = network + 1, Last usable = broadcast - 1
		firstUsable := make(net.IP, 4)
		lastUsable := make(net.IP, 4)
		binary.BigEndian.PutUint32(firstUsable, n+1)
		binary.BigEndian.PutUint32(lastUsable, (n|^mask)-1)

		return fmt.Sprintf("CIDR: %s\n\nNetwork: %s\nBroadcast: %s\nSubnet Mask: %s\nPrefix Length: /%d\nWildcard: %s\nTotal Addresses: %d\nUsable Hosts: %d\nFirst Usable: %s\nLast Usable: %s",
			cidr, network, broadcast,
			net.IP(ipNet.Mask).String(), ones,
			wildcardMask(ipNet.Mask),
			totalHosts, usableHosts,
			firstUsable, lastUsable), nil
	}

	return fmt.Sprintf("CIDR: %s\nNetwork: %s\nPrefix Length: /%d\nTotal Addresses: 2^%d",
		cidr, network, ones, hostBits), nil
}

func (p *IpProfile) ipInRange(args map[string]interface{}) (string, error) {
	ipStr := getStr(args, "ip")
	cidr := getStr(args, "cidr")
	if ipStr == "" || cidr == "" {
		return "", fmt.Errorf("ip and cidr are required")
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", fmt.Errorf("invalid IP: %s", ipStr)
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR: %s", err)
	}
	if ipNet.Contains(ip) {
		return fmt.Sprintf("YES — %s is within %s", ipStr, cidr), nil
	}
	return fmt.Sprintf("NO — %s is NOT within %s", ipStr, cidr), nil
}

func (p *IpProfile) subnetCalculator(args map[string]interface{}) (string, error) {
	networkStr := getStr(args, "network")
	hostsNeeded := int(getFloat(args, "hosts_needed"))
	if networkStr == "" {
		return "", fmt.Errorf("network is required")
	}
	if hostsNeeded <= 0 {
		return "", fmt.Errorf("hosts_needed must be positive")
	}

	_, ipNet, err := net.ParseCIDR(networkStr)
	if err != nil {
		return "", fmt.Errorf("invalid network: %s", err)
	}

	// Calculate required prefix length (need hosts + 2 for network/broadcast)
	needed := hostsNeeded + 2
	hostBits := bits.Len(uint(needed - 1))
	if hostBits < 2 {
		hostBits = 2
	}
	newPrefix := 32 - hostBits
	_, origBits := ipNet.Mask.Size()
	if origBits != 32 {
		return "", fmt.Errorf("subnet calculator only supports IPv4")
	}

	actualHosts := (1 << hostBits) - 2
	newMask := net.CIDRMask(newPrefix, 32)

	return fmt.Sprintf("Subnet Calculator:\n\nBase Network: %s\nHosts Needed: %d\n\nRecommended Subnet: /%d\nSubnet Mask: %s\nUsable Hosts Per Subnet: %d\nTotal Addresses Per Subnet: %d",
		networkStr, hostsNeeded, newPrefix, net.IP(newMask).String(), actualHosts, 1<<hostBits), nil
}

func wildcardMask(mask net.IPMask) string {
	wc := make(net.IP, len(mask))
	for i := range mask {
		wc[i] = ^mask[i]
	}
	return wc.String()
}
