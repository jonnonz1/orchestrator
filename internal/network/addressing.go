package network

import (
	"fmt"
	"hash/fnv"
)

const maxTAPLen = 15

// NetworkConfig holds all network configuration for a VM.
type NetworkConfig struct {
	TapDev    string
	TapIP     string
	GuestIP   string
	Subnet    string
	HostIface string
}

// AllocateNetwork derives the network configuration for a VM name.
func AllocateNetwork(vmName string, hostIface string) NetworkConfig {
	slot := NetSlot(vmName)
	return NetworkConfig{
		TapDev:    TAPName(vmName),
		TapIP:     fmt.Sprintf("172.16.%d.1", slot),
		GuestIP:   fmt.Sprintf("172.16.%d.2", slot),
		Subnet:    fmt.Sprintf("172.16.%d.0/24", slot),
		HostIface: hostIface,
	}
}

// HashName returns a deterministic uint32 hash from a VM name.
func HashName(name string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	return h.Sum32()
}

// NetSlot derives the network slot (1-253) from a VM name.
func NetSlot(name string) int {
	return int(HashName(name)%253) + 1
}

// AutoCID derives a vsock CID (3-65532) from a VM name.
func AutoCID(name string) uint32 {
	return (HashName(name) % 65530) + 3
}

// TAPName derives the TAP device name from a VM name, truncated to 15 chars.
func TAPName(name string) string {
	tap := "fc-" + name
	if len(tap) > maxTAPLen {
		tap = tap[:maxTAPLen]
	}
	return tap
}
