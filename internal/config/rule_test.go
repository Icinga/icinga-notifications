package config

import (
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/stretchr/testify/require"
)

const defaultDivisor = 3 // Every third rule gets a valid escalation condition.

func TestRuleEntries(t *testing.T) {
	t.Parallel()

	runtimeConfigTest := new(RuntimeConfig)
	runtimeConfigTest.Rules = make(map[int64]*rule.Rule)
	for i := 1; i <= 50; i++ {
		runtimeConfigTest.Rules[int64(i)] = makeRule(t, i)
	}

	t.Run("Evaluate", func(t *testing.T) {
		t.Parallel()

		runtime := new(RuntimeConfig)
		runtime.Rules = maps.Clone(runtimeConfigTest.Rules)

		e := make(RuleEntries)

		expectedLen := 0
		filterContext := &rule.EscalationFilter{IncidentSeverity: event.SeverityEmerg}
		assertEntries := func(rules map[int64]struct{}, expectedLen *int, expectError bool, opts EvalOptions) {
			if expectError {
				require.Error(t, e.Evaluate(runtime, filterContext, rules, opts))
			} else {
				require.NoError(t, e.Evaluate(runtime, filterContext, rules, opts))
			}
			require.Len(t, e, *expectedLen)
			clear(e) // Clear the entries for the next run.
		}
		expectedLen = len(runtime.Rules)/defaultDivisor - 5 // 15/3 => (5) valid entries are going to be deleted below.

		// Drop some random rules from the runtime config to simulate a runtime config deletion!
		maps.DeleteFunc(runtime.Rules, func(ruleID int64, _ *rule.Rule) bool { return ruleID > 35 && ruleID%defaultDivisor == 0 })

		opts := EvalOptions{
			OnPreEvaluate: func(re *rule.Escalation) bool {
				if re.RuleID > 35 && re.RuleID%defaultDivisor == 0 { // Those rules are deleted from our runtime config.
					require.Failf(t, "OnPreEvaluate() shouldn't have been called", "rule %d was deleted from runtime config", re.RuleID)
				}
				require.Nilf(t, e[re.ID], "Evaluate() shouldn't evaluate entry %d twice", re.ID)
				return true
			},
			OnError: func(re *rule.Escalation, err error) bool {
				require.EqualError(t, err, `unknown severity "evaluable"`)
				return true
			},
			OnFilterMatch: func(re *rule.Escalation) error {
				require.Nilf(t, e[re.ID], "OnPreEvaluate() shouldn't evaluate %d twice", re.ID)
				return nil
			},
		}
		rules := ruleIDs(runtime)
		assertEntries(rules, &expectedLen, false, opts)

		lenBeforeError := new(int)
		opts.OnError = func(re *rule.Escalation, err error) bool {
			if *lenBeforeError != 0 {
				require.Fail(t, "OnError() shouldn't have been called again")
			}
			require.EqualError(t, err, `unknown severity "evaluable"`)

			*lenBeforeError = len(e)
			return false // This should let the evaluation fail completely!
		}
		assertEntries(rules, lenBeforeError, true, opts)

		*lenBeforeError = 0
		opts.OnError = nil
		opts.OnFilterMatch = func(re *rule.Escalation) error {
			if *lenBeforeError != 0 {
				require.Fail(t, "OnFilterMatch() shouldn't have been called again")
			}

			*lenBeforeError = len(e)
			return fmt.Errorf("OnFilterMatch() failed badly") // This should let the evaluation fail completely!
		}
		assertEntries(rules, lenBeforeError, true, opts)

		expectedLen = 0
		filterContext.IncidentSeverity = 1 // OK
		filterContext.IncidentAge = 5 * time.Minute

		opts.OnFilterMatch = nil
		opts.OnPreEvaluate = func(re *rule.Escalation) bool { return re.RuleID < 5 }
		opts.OnAllConfigEvaluated = func(result time.Duration) {
			// The filter string of the escalation condition is incident_age>=10m and the actual incident age is 5m.
			require.Equal(t, 5*time.Minute, result)
		}
		assertEntries(rules, &expectedLen, false, opts)
	})
}

// makeRule creates a rule with some escalation entries.
//
// Every rule gets one invalid escalation condition that always fails to evaluate.
// Additionally, every third (defaultDivisor) rule gets a valid escalation condition that matches
// on `incident_severity>warning||incident_age>=10m` to simulate some real-world conditions.
func makeRule(t *testing.T, i int) *rule.Rule {
	r := new(rule.Rule)
	r.ID = int64(i)
	r.Name = fmt.Sprintf("rule-%d", i)
	r.Escalations = make(map[int64]*rule.Escalation)

	invalidSeverity, err := filter.Parse("incident_severity=evaluable")
	require.NoError(t, err, "parsing incident_severity=evaluable shouldn't fail")

	redundant := new(rule.Escalation)
	redundant.ID = r.ID * 150 // It must be large enough to avoid colliding with others!
	redundant.RuleID = r.ID
	redundant.Condition = invalidSeverity

	r.Escalations[redundant.ID] = redundant
	if i%defaultDivisor == 0 {
		escalationCond, err := filter.Parse("incident_severity>warning||incident_age>=10m")
		require.NoError(t, err, "parsing incident_severity>warning||incident_age>=10m shouldn't fail")

		entry := new(rule.Escalation)
		entry.ID = r.ID * 2
		entry.RuleID = r.ID
		entry.Condition = escalationCond

		r.Escalations[entry.ID] = entry
	}

	return r
}

// ruleIDs extracts the rule IDs from the given RuntimeConfig and returns them as a map[int64]struct{}.
func ruleIDs(r *RuntimeConfig) map[int64]struct{} {
	ids := make(map[int64]struct{}, len(r.Rules))
	for id := range r.Rules {
		ids[id] = struct{}{}
	}
	return ids
}
