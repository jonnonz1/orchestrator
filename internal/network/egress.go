package network

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/coreos/go-iptables/iptables"
)

// EgressPolicy controls what outbound traffic a VM is allowed to make.
// When Allowlist is empty, all traffic is allowed (default, backwards-compat).
// When populated, only traffic to the listed destinations + DNS is allowed.
type EgressPolicy struct {
	// Allowlist is a list of IP addresses, CIDRs, or hostnames. Hostnames are
	// resolved once at setup time and all resulting IPs are allowed.
	Allowlist []string
}

// ParseEgressAllowlist splits a comma-separated env-var value into an
// EgressPolicy. Empty string → empty allowlist → unrestricted.
func ParseEgressAllowlist(raw string) EgressPolicy {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return EgressPolicy{}
	}
	var entries []string
	for _, e := range strings.Split(raw, ",") {
		e = strings.TrimSpace(e)
		if e != "" {
			entries = append(entries, e)
		}
	}
	return EgressPolicy{Allowlist: entries}
}

// SetupEgress applies iptables rules on the FORWARD chain to restrict what a
// VM's TAP interface can reach. It:
//
//  1. Allows DNS (UDP 53) to any destination (so the guest can resolve names).
//  2. Allows traffic to the Anthropic API (api.anthropic.com) since that's
//     almost always needed.
//  3. Allows each entry in the allowlist (resolving hostnames to IPs).
//  4. Drops all other FORWARD traffic originating from the TAP.
//
// Rules are inserted at position 1 (for UFW compatibility) and tagged with
// a comment for easy teardown.
func SetupEgress(tapDev string, policy EgressPolicy) error {
	if len(policy.Allowlist) == 0 {
		return nil
	}

	ipt, err := iptables.New()
	if err != nil {
		return fmt.Errorf("egress: init iptables: %w", err)
	}

	tag := fmt.Sprintf("orch-egress-%s", tapDev)

	if err := ipt.Insert("filter", "FORWARD", 1,
		"-i", tapDev, "-p", "udp", "--dport", "53",
		"-m", "comment", "--comment", tag,
		"-j", "ACCEPT"); err != nil {
		return fmt.Errorf("egress: allow DNS: %w", err)
	}

	if err := ipt.Insert("filter", "FORWARD", 1,
		"-i", tapDev, "-p", "tcp", "--dport", "53",
		"-m", "comment", "--comment", tag,
		"-j", "ACCEPT"); err != nil {
		return fmt.Errorf("egress: allow DNS/TCP: %w", err)
	}

	allDests := append([]string{"api.anthropic.com"}, policy.Allowlist...)

	for _, entry := range allDests {
		ips, err := resolveToIPs(entry)
		if err != nil {
			return fmt.Errorf("egress: resolve %q: %w", entry, err)
		}
		for _, ip := range ips {
			if err := ipt.Insert("filter", "FORWARD", 1,
				"-i", tapDev, "-d", ip,
				"-m", "comment", "--comment", tag,
				"-j", "ACCEPT"); err != nil {
				return fmt.Errorf("egress: allow %s (%s): %w", entry, ip, err)
			}
		}
	}

	if err := ipt.Append("filter", "FORWARD",
		"-i", tapDev,
		"-m", "comment", "--comment", tag,
		"-j", "DROP"); err != nil {
		return fmt.Errorf("egress: default DROP: %w", err)
	}

	return nil
}

// TeardownEgress removes all egress rules for a given TAP device.
func TeardownEgress(tapDev string) error {
	tag := fmt.Sprintf("orch-egress-%s", tapDev)
	out, err := exec.Command("iptables-save").Output()
	if err != nil {
		return fmt.Errorf("egress teardown: iptables-save: %w", err)
	}

	ipt, err := iptables.New()
	if err != nil {
		return fmt.Errorf("egress teardown: init iptables: %w", err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, tag) || !strings.HasPrefix(line, "-A FORWARD") {
			continue
		}
		args := strings.Fields(strings.TrimPrefix(line, "-A FORWARD "))
		ipt.Delete("filter", "FORWARD", args...)
	}
	return nil
}

// resolveToIPs returns CIDR strings for the given entry:
//   - Already an IP → "1.2.3.4/32"
//   - Already a CIDR → returned as-is
//   - Hostname → resolve via net.LookupHost, return all IPs as /32
func resolveToIPs(entry string) ([]string, error) {
	if _, _, err := net.ParseCIDR(entry); err == nil {
		return []string{entry}, nil
	}
	if ip := net.ParseIP(entry); ip != nil {
		if ip.To4() != nil {
			return []string{ip.String() + "/32"}, nil
		}
		return []string{ip.String() + "/128"}, nil
	}
	addrs, err := net.LookupHost(entry)
	if err != nil {
		return nil, err
	}
	var ips []string
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		if ip.To4() != nil {
			ips = append(ips, a+"/32")
		} else {
			ips = append(ips, a+"/128")
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IPs resolved for %q", entry)
	}
	return ips, nil
}
