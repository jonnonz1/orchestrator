package network

import (
	"fmt"

	"github.com/coreos/go-iptables/iptables"
)

// SetupNAT configures iptables NAT and FORWARD rules for a VM.
// FORWARD rules are inserted at position 1 for UFW compatibility.
// Both the NAT rule and the FORWARD rules are made idempotent with an
// Exists check so that retries do not leak duplicate rules.
func SetupNAT(cfg NetworkConfig) error {
	ipt, err := iptables.New()
	if err != nil {
		return fmt.Errorf("init iptables: %w", err)
	}

	// NAT: masquerade traffic from VM subnet
	if err := ipt.AppendUnique("nat", "POSTROUTING",
		"-s", cfg.Subnet, "-o", cfg.HostIface, "-j", "MASQUERADE"); err != nil {
		return fmt.Errorf("add NAT rule: %w", err)
	}

	forwardRules := [][]string{
		// outbound from TAP
		{"-i", cfg.TapDev, "-o", cfg.HostIface, "-j", "ACCEPT"},
		// established/related inbound to TAP
		{"-i", cfg.HostIface, "-o", cfg.TapDev,
			"-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
	}
	for _, rule := range forwardRules {
		exists, err := ipt.Exists("filter", "FORWARD", rule...)
		if err != nil {
			return fmt.Errorf("check FORWARD rule: %w", err)
		}
		if exists {
			continue
		}
		if err := ipt.Insert("filter", "FORWARD", 1, rule...); err != nil {
			return fmt.Errorf("add FORWARD rule: %w", err)
		}
	}

	return nil
}

// TeardownNAT removes iptables NAT and FORWARD rules for a VM.
func TeardownNAT(cfg NetworkConfig) error {
	ipt, err := iptables.New()
	if err != nil {
		return fmt.Errorf("init iptables: %w", err)
	}

	// Best-effort removal — ignore errors for rules that may already be gone
	ipt.Delete("nat", "POSTROUTING",
		"-s", cfg.Subnet, "-o", cfg.HostIface, "-j", "MASQUERADE")

	ipt.Delete("filter", "FORWARD",
		"-i", cfg.TapDev, "-o", cfg.HostIface, "-j", "ACCEPT")

	ipt.Delete("filter", "FORWARD",
		"-i", cfg.HostIface, "-o", cfg.TapDev,
		"-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT")

	return nil
}
