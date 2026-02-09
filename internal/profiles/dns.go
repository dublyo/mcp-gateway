package profiles

import (
	"fmt"
	"net"
	"strings"
	"time"
)

type DnsProfile struct{}

func (p *DnsProfile) ID() string { return "dns" }

func (p *DnsProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "dns_lookup",
			Description: "Look up DNS records for a domain (A, AAAA, MX, TXT, CNAME, NS)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"domain": map[string]interface{}{"type": "string", "description": "Domain name to look up"},
					"record_type": map[string]interface{}{
						"type":        "string",
						"description": "Record type: A, AAAA, MX, TXT, CNAME, NS, ALL (default ALL)",
						"default":     "ALL",
					},
				},
				"required": []string{"domain"},
			},
		},
		{
			Name:        "reverse_lookup",
			Description: "Perform reverse DNS lookup for an IP address",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"ip": map[string]interface{}{"type": "string", "description": "IP address to look up"},
				},
				"required": []string{"ip"},
			},
		},
		{
			Name:        "check_port",
			Description: "Check if a TCP port is open on a host",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"host": map[string]interface{}{"type": "string", "description": "Hostname or IP address"},
					"port": map[string]interface{}{"type": "integer", "description": "Port number to check"},
				},
				"required": []string{"host", "port"},
			},
		},
		{
			Name:        "resolve_host",
			Description: "Resolve a hostname to all its IP addresses",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"host": map[string]interface{}{"type": "string", "description": "Hostname to resolve"},
				},
				"required": []string{"host"},
			},
		},
	}
}

func (p *DnsProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "dns_lookup":
		return p.dnsLookup(args)
	case "reverse_lookup":
		return p.reverseLookup(args)
	case "check_port":
		return p.checkPort(args)
	case "resolve_host":
		return p.resolveHost(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *DnsProfile) dnsLookup(args map[string]interface{}) (string, error) {
	domain := getStr(args, "domain")
	if domain == "" {
		return "", fmt.Errorf("domain is required")
	}
	recordType := strings.ToUpper(getStr(args, "record_type"))
	if recordType == "" {
		recordType = "ALL"
	}

	var sections []string

	if recordType == "ALL" || recordType == "A" {
		ips, err := net.LookupHost(domain)
		if err == nil && len(ips) > 0 {
			var aRecords, aaaaRecords []string
			for _, ip := range ips {
				if net.ParseIP(ip).To4() != nil {
					aRecords = append(aRecords, ip)
				} else {
					aaaaRecords = append(aaaaRecords, ip)
				}
			}
			if len(aRecords) > 0 {
				sections = append(sections, fmt.Sprintf("A Records:\n  %s", strings.Join(aRecords, "\n  ")))
			}
			if (recordType == "ALL" || recordType == "AAAA") && len(aaaaRecords) > 0 {
				sections = append(sections, fmt.Sprintf("AAAA Records:\n  %s", strings.Join(aaaaRecords, "\n  ")))
			}
		}
	}

	if recordType == "ALL" || recordType == "MX" {
		mxs, err := net.LookupMX(domain)
		if err == nil && len(mxs) > 0 {
			var lines []string
			for _, mx := range mxs {
				lines = append(lines, fmt.Sprintf("%s (priority %d)", mx.Host, mx.Pref))
			}
			sections = append(sections, fmt.Sprintf("MX Records:\n  %s", strings.Join(lines, "\n  ")))
		}
	}

	if recordType == "ALL" || recordType == "TXT" {
		txts, err := net.LookupTXT(domain)
		if err == nil && len(txts) > 0 {
			sections = append(sections, fmt.Sprintf("TXT Records:\n  %s", strings.Join(txts, "\n  ")))
		}
	}

	if recordType == "ALL" || recordType == "CNAME" {
		cname, err := net.LookupCNAME(domain)
		if err == nil && cname != "" && cname != domain+"." {
			sections = append(sections, fmt.Sprintf("CNAME: %s", cname))
		}
	}

	if recordType == "ALL" || recordType == "NS" {
		nss, err := net.LookupNS(domain)
		if err == nil && len(nss) > 0 {
			var lines []string
			for _, ns := range nss {
				lines = append(lines, ns.Host)
			}
			sections = append(sections, fmt.Sprintf("NS Records:\n  %s", strings.Join(lines, "\n  ")))
		}
	}

	if len(sections) == 0 {
		return fmt.Sprintf("No %s records found for %s", recordType, domain), nil
	}
	return fmt.Sprintf("DNS lookup for %s:\n\n%s", domain, strings.Join(sections, "\n\n")), nil
}

func (p *DnsProfile) reverseLookup(args map[string]interface{}) (string, error) {
	ip := getStr(args, "ip")
	if ip == "" {
		return "", fmt.Errorf("ip is required")
	}
	names, err := net.LookupAddr(ip)
	if err != nil {
		return "", fmt.Errorf("reverse lookup failed: %s", err)
	}
	if len(names) == 0 {
		return fmt.Sprintf("No reverse DNS entries for %s", ip), nil
	}
	return fmt.Sprintf("Reverse DNS for %s:\n  %s", ip, strings.Join(names, "\n  ")), nil
}

func (p *DnsProfile) checkPort(args map[string]interface{}) (string, error) {
	host := getStr(args, "host")
	if host == "" {
		return "", fmt.Errorf("host is required")
	}
	port := int(getFloat(args, "port"))
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		return fmt.Sprintf("Port %d on %s: CLOSED (timeout: %s)\nError: %s", port, host, elapsed.Round(time.Millisecond), err), nil
	}
	conn.Close()
	return fmt.Sprintf("Port %d on %s: OPEN (response time: %s)", port, host, elapsed.Round(time.Millisecond)), nil
}

func (p *DnsProfile) resolveHost(args map[string]interface{}) (string, error) {
	host := getStr(args, "host")
	if host == "" {
		return "", fmt.Errorf("host is required")
	}
	ips, err := net.LookupHost(host)
	if err != nil {
		return "", fmt.Errorf("resolve failed: %s", err)
	}
	return fmt.Sprintf("Host %s resolves to:\n  %s", host, strings.Join(ips, "\n  ")), nil
}

func getFloat(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
