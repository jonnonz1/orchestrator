package inject

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InjectFiles mounts the rootfs image and writes files into it.
// This must be called before the VM boots. guestPath entries must be absolute
// paths inside the guest (they will be joined to the mount point) and must
// not contain `..` traversal segments — anything that would escape the
// mount point is rejected.
func InjectFiles(rootfsPath string, files map[string][]byte) error {
	mountDir, err := os.MkdirTemp("", "fc-mount-")
	if err != nil {
		return fmt.Errorf("create temp mount dir: %w", err)
	}
	defer os.RemoveAll(mountDir)

	if err := exec.Command("mount", rootfsPath, mountDir).Run(); err != nil {
		return fmt.Errorf("mount %s: %w", rootfsPath, err)
	}
	defer exec.Command("umount", mountDir).Run()

	for guestPath, content := range files {
		safe, err := safeJoin(mountDir, guestPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", guestPath, err)
		}
		if err := os.WriteFile(safe, content, 0644); err != nil {
			return fmt.Errorf("write %s: %w", guestPath, err)
		}
	}

	return nil
}

// safeJoin returns `filepath.Join(base, target)` after checking that the
// resulting cleaned path is still contained inside base. This prevents
// traversal via `..` segments in guest-file paths.
func safeJoin(base, target string) (string, error) {
	cleaned := filepath.Clean(filepath.Join(base, target))
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve base: %w", err)
	}
	absCleaned, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve target: %w", err)
	}
	if !strings.HasPrefix(absCleaned, absBase+string(os.PathSeparator)) && absCleaned != absBase {
		return "", fmt.Errorf("path %q escapes mount", target)
	}
	return absCleaned, nil
}

// InjectNetworkConfig writes systemd-networkd config and hostname into the rootfs.
func InjectNetworkConfig(rootfsPath, guestIP, tapIP, hostname string) error {
	networkCfg := fmt.Sprintf(`[Match]
Name=eth0

[Network]
Address=%s/24
Gateway=%s
DNS=8.8.8.8
DNS=8.8.4.4
`, guestIP, tapIP)

	resolvConf := "nameserver 8.8.8.8\nnameserver 8.8.4.4\n"

	return InjectFiles(rootfsPath, map[string][]byte{
		"/etc/systemd/network/20-eth0.network": []byte(networkCfg),
		"/etc/resolv.conf":                     []byte(resolvConf),
		"/etc/hostname":                        []byte(hostname + "\n"),
	})
}
