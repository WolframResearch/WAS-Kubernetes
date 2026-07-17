package fixers

import (
	"github.com/WolframResearch/WAS-Kubernetes/internal/doctor"
)

// SafeFixers returns all checks from the registry that have SafeToFix == true.
// No registry checks currently opt into automatic repair.
func SafeFixers(checks []doctor.Check) []doctor.Check {
	var out []doctor.Check
	for _, c := range checks {
		if c.SafeToFix() {
			out = append(out, c)
		}
	}
	return out
}
