package network

import "testing"

func TestHashName_Deterministic(t *testing.T) {
	h1 := HashName("test1")
	h2 := HashName("test1")
	if h1 != h2 {
		t.Errorf("hash not deterministic: %d != %d", h1, h2)
	}
}

func TestHashName_Different(t *testing.T) {
	h1 := HashName("test1")
	h2 := HashName("test2")
	if h1 == h2 {
		t.Errorf("different names produced same hash: %d", h1)
	}
}

func TestNetSlot_Range(t *testing.T) {
	names := []string{"test1", "test2", "my-vm", "task-abc123", "a", "zzzzz"}
	for _, name := range names {
		slot := NetSlot(name)
		if slot < 1 || slot > 253 {
			t.Errorf("NetSlot(%q) = %d, want 1-253", name, slot)
		}
	}
}

func TestAutoCID_Range(t *testing.T) {
	names := []string{"test1", "test2", "my-vm", "task-abc123"}
	for _, name := range names {
		cid := AutoCID(name)
		if cid < 3 || cid > 65532 {
			t.Errorf("AutoCID(%q) = %d, want 3-65532", name, cid)
		}
	}
}

func TestTAPName_MaxLength(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"test1", "fc-test1"},
		{"a", "fc-a"},
		{"very-long-vm-name-that-exceeds", "fc-very-long-vm"},
	}

	for _, tt := range tests {
		got := TAPName(tt.name)
		if got != tt.want {
			t.Errorf("TAPName(%q) = %q, want %q", tt.name, got, tt.want)
		}
		if len(got) > 15 {
			t.Errorf("TAPName(%q) length %d > 15", tt.name, len(got))
		}
	}
}

func TestAllocateNetwork(t *testing.T) {
	cfg := AllocateNetwork("test1", "eth0")

	if cfg.TapDev == "" {
		t.Error("TapDev is empty")
	}
	if cfg.TapIP == "" {
		t.Error("TapIP is empty")
	}
	if cfg.GuestIP == "" {
		t.Error("GuestIP is empty")
	}
	if cfg.Subnet == "" {
		t.Error("Subnet is empty")
	}
	if cfg.HostIface != "eth0" {
		t.Errorf("HostIface = %q, want eth0", cfg.HostIface)
	}

	// TapIP and GuestIP should be in the same /24
	if cfg.TapIP[:len(cfg.TapIP)-1] != cfg.GuestIP[:len(cfg.GuestIP)-1] {
		t.Errorf("TapIP %s and GuestIP %s not in same subnet", cfg.TapIP, cfg.GuestIP)
	}
}
