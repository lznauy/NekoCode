package subagent

import (
	"fmt"
	"strings"

	"nekocode/bot/provider/types"
)

const maxVerificationChecks = 32

// VerificationCheck describes evidence coverage, not the task lifecycle or a
// host-certified verdict. A passing probe may still model the wrong scope.
type VerificationCheck struct {
	Name, Status, Evidence, Entrypoint, Scope string
	Limitations                               []string
}

func verificationChecksSchema() types.Property {
	return types.Property{Type: "array", Description: "At most 32 scoped checks. blocked means environment prevented execution. A synthetic probe alone does not prove production reachability.", Items: &types.Property{Type: "object", Properties: map[string]types.Property{
		"name":        {Type: "string"},
		"status":      {Type: "string", Enum: []string{"passed", "failed", "blocked", "not_run"}},
		"evidence":    {Type: "string", Description: "Command/assertion and result; repair claims need failing-before/passing-after controls."},
		"entrypoint":  {Type: "string", Description: "Actual entrypoint; identify helper/synthetic probes."},
		"scope":       {Type: "string", Description: "Ownership, concurrency, branches reached, competing guards and applicability."},
		"limitations": {Type: "array", Items: &types.Property{Type: "string"}, Description: "Unverified assumptions and missing coverage within scope."},
	}, Required: []string{"name", "status", "evidence", "entrypoint", "scope", "limitations"}}}
}

func optionalVerificationChecks(args map[string]any) ([]VerificationCheck, error) {
	raw, present := args["verification_checks"]
	if !present {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok || len(items) > maxVerificationChecks {
		return nil, fmt.Errorf("verification_checks must be an array of at most %d checks", maxVerificationChecks)
	}
	checks := make([]VerificationCheck, 0, len(items))
	total := 0
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("verification_checks entries must be objects")
		}
		var check VerificationCheck
		for _, field := range []struct {
			name   string
			target *string
		}{{"name", &check.Name}, {"status", &check.Status}, {"evidence", &check.Evidence}, {"entrypoint", &check.Entrypoint}, {"scope", &check.Scope}} {
			text, err := requiredResultString(fields, field.name)
			if err != nil {
				return nil, fmt.Errorf("verification_checks: %w", err)
			}
			if len(text) > 4096 {
				return nil, fmt.Errorf("verification_checks.%s exceeds 4096 bytes", field.name)
			}
			*field.target = text
			total += len(text)
		}
		switch check.Status {
		case "passed", "failed", "blocked", "not_run":
		default:
			return nil, fmt.Errorf("invalid verification status %q", check.Status)
		}
		var err error
		check.Limitations, err = requiredResultStrings(fields, "limitations")
		if err != nil {
			return nil, fmt.Errorf("verification_checks: %w", err)
		}
		if len(check.Limitations) > 16 {
			return nil, fmt.Errorf("too many verification limitations")
		}
		for _, text := range check.Limitations {
			total += len(text)
		}
		if total > 32<<10 {
			return nil, fmt.Errorf("verification_checks exceeds 32 KiB")
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func writeVerificationChecks(out *strings.Builder, checks []VerificationCheck) {
	out.WriteString("\n\nVerification coverage (worker-reported, scoped evidence; not proof of full validation):\n")
	if len(checks) == 0 {
		out.WriteString("- Not reported. Do not infer verification passed from task completion.")
		return
	}
	counts := map[string]int{}
	for _, check := range checks {
		counts[check.Status]++
	}
	fmt.Fprintf(out, "- passed=%d; failed=%d; blocked=%d; not_run=%d", counts["passed"], counts["failed"], counts["blocked"], counts["not_run"])
	for _, check := range checks {
		fmt.Fprintf(out, "\n- [%s] %s\n  Evidence: %s\n  Entrypoint: %s\n  Scope: %s", check.Status, check.Name, check.Evidence, check.Entrypoint, check.Scope)
		for _, limit := range check.Limitations {
			fmt.Fprintf(out, "\n  Limitation: %s", limit)
		}
	}
}
