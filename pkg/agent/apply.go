package agent

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os/exec"
)

// ApplyConfigs tries to apply the generated configs.
// It assumes wg-quick and vtysh are installed and the caller has sufficient privileges.
func ApplyConfigs(wgConfPath, iface string, bgpConfPath string) error {
	if iface == "" {
		iface = "wg0"
	}

	if err := applyWireGuard(wgConfPath, iface); err != nil {
		return err
	}
	if err := ensureNAT(iface); err != nil {
		log.Printf("ensure NAT failed: %v", err)
	}
	if err := run("vtysh", "-b", "-f", bgpConfPath); err != nil {
		return fmt.Errorf("vtysh apply bgp: %w", err)
	}
	return nil
}

// applyWireGuard updates the interface without tearing it down when possible to avoid flaps.
func applyWireGuard(wgConfPath, iface string) error {
	if !ifaceExists(iface) {
		if err := run("wg-quick", "up", wgConfPath); err != nil {
			return fmt.Errorf("wg-quick up: %w", err)
		}
		return nil
	}

	// Interface exists: update peers using wg syncconf + wg-quick strip to avoid removing the interface.
	stripCmd := exec.Command("wg-quick", "strip", wgConfPath)
	conf, err := stripCmd.Output()
	if err != nil {
		return fmt.Errorf("wg-quick strip: %w", err)
	}

	cmd := exec.Command("wg", "syncconf", iface, "/dev/stdin")
	cmd.Stdin = bytes.NewReader(conf)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wg syncconf: %w output=%s", err, string(out))
	}
	return nil
}

func ifaceExists(iface string) bool {
	if iface == "" {
		return false
	}
	_, err := net.InterfaceByName(iface)
	return err == nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %v output=%s", name, args, err, string(out))
	}
	return nil
}
