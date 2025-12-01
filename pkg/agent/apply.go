package agent

import (
	"fmt"
	"os/exec"
)

// ApplyConfigs tries to apply the generated configs.
// It assumes wg-quick and vtysh are installed and the caller has sufficient privileges.
func ApplyConfigs(wgConfPath, iface string, bgpConfPath string) error {
	if iface == "" {
		iface = "wg0"
	}
	if err := run("wg-quick", "down", wgConfPath); err != nil {
		// ignore errors on down; interface may not exist
	}
	if err := run("wg-quick", "up", wgConfPath); err != nil {
		return fmt.Errorf("wg-quick up: %w", err)
	}
	if err := run("vtysh", "-b", "-f", bgpConfPath); err != nil {
		return fmt.Errorf("vtysh apply bgp: %w", err)
	}
	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %v output=%s", name, args, err, string(out))
	}
	return nil
}
