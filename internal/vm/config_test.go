package vm

import "testing"

func TestVMConfig_Defaults(t *testing.T) {
	cfg := VMConfig{Name: "test"}
	cfg.Defaults()

	if cfg.RamMB != 512 {
		t.Errorf("RamMB = %d, want 512", cfg.RamMB)
	}
	if cfg.VCPUs != 1 {
		t.Errorf("VCPUs = %d, want 1", cfg.VCPUs)
	}
}

func TestVMConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     VMConfig
		wantErr bool
	}{
		{"valid", VMConfig{Name: "test1", RamMB: 512, VCPUs: 1}, false},
		{"valid-hyphen", VMConfig{Name: "my-vm", RamMB: 512, VCPUs: 1}, false},
		{"empty-name", VMConfig{Name: "", RamMB: 512, VCPUs: 1}, true},
		{"hyphen-start", VMConfig{Name: "-bad", RamMB: 512, VCPUs: 1}, true},
		{"special-chars", VMConfig{Name: "bad@name", RamMB: 512, VCPUs: 1}, true},
		{"low-ram", VMConfig{Name: "test", RamMB: 64, VCPUs: 1}, true},
		{"high-ram", VMConfig{Name: "test", RamMB: 99999, VCPUs: 1}, true},
		{"zero-vcpus", VMConfig{Name: "test", RamMB: 512, VCPUs: 0}, true},
		{"high-vcpus", VMConfig{Name: "test", RamMB: 512, VCPUs: 64}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
