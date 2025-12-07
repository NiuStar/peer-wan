package agent

import (
	"fmt"
	"os/exec"
	"strings"

	"peer-wan/pkg/model"
	"peer-wan/pkg/policy"
)

// collectVerifyTargets gathers IP/domains from policy rules for final verification.
func collectVerifyTargets(rules []model.PolicyRule) []string {
	targets := []string{}
	seen := map[string]struct{}{}
	for _, pr := range rules {
		if pr.Prefix != "" {
			target := pr.Prefix
			if strings.Contains(target, "/") {
				target = strings.Split(target, "/")[0]
			}
			if _, ok := seen[target]; !ok {
				seen[target] = struct{}{}
				targets = append(targets, target)
			}
		}
		for _, d := range pr.Domains {
			d = strings.TrimSpace(d)
			if d != "" {
				if _, ok := seen[d]; !ok {
					seen[d] = struct{}{}
					targets = append(targets, d)
				}
			}
		}
		// expand geoip/domains to IPs best effort
		for _, p := range policy.Expand(pr) {
			if strings.Contains(p, "/") {
				p = strings.Split(p, "/")[0]
			}
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				targets = append(targets, p)
			}
		}
	}
	return targets
}

func runCurlVerify(targets []string) error {
	for _, t := range targets {
		arg := t
		// prefer http:// if no scheme
		if !strings.Contains(arg, "://") {
			arg = "http://" + arg
		}
		cmd := exec.Command("curl", "-4", "-m", "5", "-sSf", arg)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("curl %s failed: %v (%s)", arg, err, string(out))
		}
	}
	return nil
}
