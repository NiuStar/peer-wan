package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultOverlayCIDR = "10.10.0.0/16"
const natStatePath = "/var/lib/peer-wan/nat_state.json"

// ensureNAT best-effort installs forwarding + MASQUERADE so overlay traffic can egress without manual iptables.
// It mirrors the bootstrap script behavior but runs every apply to keep rules present.
func ensureNAT(iface string) error {
	if runtime.GOOS == "darwin" {
		// macOS lacks iptables; nothing to do
		return nil
	}
	if iface == "" {
		iface = "wg0"
	}
	if strings.EqualFold(os.Getenv("AUTO_NAT"), "false") {
		return nil
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		log.Printf("iptables not found, skip NAT setup")
		return nil
	}

	cidr := os.Getenv("WG_CIDR")
	if cidr == "" {
		cidr = defaultOverlayCIDR
	}
	egress := os.Getenv("NAT_EGRESS_IF")
	if egress == "" {
		egress = os.Getenv("WAN_IF")
	}
	if egress == "" {
		if _, dev := detectPrimaryRoute(); dev != "" {
			egress = dev
		}
	}
	if egress == "" {
		egress = iface
	}

	prev := loadNatState()
	if prev.Iface != "" && (prev.Iface != iface || prev.Egress != egress || prev.CIDR != cidr) {
		// attempt to delete old managed rules so config changes don't leave stale entries
		if err := cleanupNatRules(prev.Iface, prev.Egress, prev.CIDR); err != nil {
			log.Printf("cleanup old NAT rules failed: %v", err)
		}
	}

	// Enable forwarding, best effort.
	_ = exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run()

	// Allow overlay -> egress and return traffic.
	if err := ensureIptablesRule([]string{"-C", "FORWARD", "-i", iface, "-o", egress, "-j", "ACCEPT"}, []string{"-A", "FORWARD", "-i", iface, "-o", egress, "-j", "ACCEPT"}); err != nil {
		return fmt.Errorf("iptables forward wg->%s: %w", egress, err)
	}
	if err := ensureIptablesRule([]string{"-C", "FORWARD", "-i", egress, "-o", iface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"}, []string{"-A", "FORWARD", "-i", egress, "-o", iface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"}); err != nil {
		return fmt.Errorf("iptables forward %s->wg: %w", egress, err)
	}

	// SNAT overlay so upstream hops/public internet can return traffic.
	if err := ensureIptablesRule(
		[]string{"-t", "nat", "-C", "POSTROUTING", "-s", cidr, "-o", egress, "-j", "MASQUERADE"},
		[]string{"-t", "nat", "-A", "POSTROUTING", "-s", cidr, "-o", egress, "-j", "MASQUERADE"},
	); err != nil {
		return fmt.Errorf("iptables masquerade %s via %s: %w", cidr, egress, err)
	}
	_ = saveNatState(natState{Iface: iface, Egress: egress, CIDR: cidr})
	log.Printf("NAT ensured for %s via %s (cidr=%s)", iface, egress, cidr)
	return nil
}

func ensureIptablesRule(checkArgs, addArgs []string) error {
	if len(checkArgs) == 0 || len(addArgs) == 0 {
		return fmt.Errorf("missing args")
	}
	if err := exec.Command("iptables", checkArgs...).Run(); err == nil {
		return nil
	}
	if out, err := exec.Command("iptables", addArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("add %v: %v (%s)", addArgs, err, string(out))
	}
	return nil
}

type natState struct {
	Iface  string `json:"iface"`
	Egress string `json:"egress"`
	CIDR   string `json:"cidr"`
}

func loadNatState() natState {
	data, err := os.ReadFile(natStatePath)
	if err != nil {
		return natState{}
	}
	var s natState
	if err := json.Unmarshal(data, &s); err != nil {
		return natState{}
	}
	return s
}

func saveNatState(s natState) error {
	if err := os.MkdirAll(filepath.Dir(natStatePath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(natStatePath, data, 0o644)
}

// cleanupNatRules removes previously managed rules when config changes.
func cleanupNatRules(iface, egress, cidr string) error {
	if iface == "" || egress == "" || cidr == "" {
		return nil
	}
	del := func(args []string) {
		_ = exec.Command("iptables", args...).Run()
	}
	del([]string{"-t", "nat", "-D", "POSTROUTING", "-s", cidr, "-o", egress, "-j", "MASQUERADE"})
	del([]string{"-D", "FORWARD", "-i", iface, "-o", egress, "-j", "ACCEPT"})
	del([]string{"-D", "FORWARD", "-i", egress, "-o", iface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"})
	return nil
}
