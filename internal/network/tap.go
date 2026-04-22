package network

import (
	"fmt"
	"os"

	"github.com/vishvananda/netlink"
)

// SetupTAP creates a TAP device with the given name and assigns an IP.
func SetupTAP(cfg NetworkConfig) error {
	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{Name: cfg.TapDev},
		Mode:      netlink.TUNTAP_MODE_TAP,
	}

	if err := netlink.LinkAdd(tap); err != nil {
		return fmt.Errorf("create TAP %s: %w", cfg.TapDev, err)
	}

	link, err := netlink.LinkByName(cfg.TapDev)
	if err != nil {
		return fmt.Errorf("find TAP %s: %w", cfg.TapDev, err)
	}

	addr, err := netlink.ParseAddr(cfg.TapIP + "/24")
	if err != nil {
		return fmt.Errorf("parse addr %s/24: %w", cfg.TapIP, err)
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("add addr to %s: %w", cfg.TapDev, err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("set %s up: %w", cfg.TapDev, err)
	}

	return nil
}

// TeardownTAP removes a TAP device.
func TeardownTAP(tapDev string) error {
	link, err := netlink.LinkByName(tapDev)
	if err != nil {
		// Already gone
		return nil
	}

	if err := netlink.LinkSetDown(link); err != nil {
		return fmt.Errorf("set %s down: %w", tapDev, err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete %s: %w", tapDev, err)
	}

	return nil
}

// EnableIPForwarding enables IPv4 forwarding on the host.
func EnableIPForwarding() error {
	return os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)
}
