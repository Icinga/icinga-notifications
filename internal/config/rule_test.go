package config

import (
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/icinga/icinga-go-library/logging"
	"github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/icinga/icinga-notifications/internal/testutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const defaultDivisor = 3 // Every third rule gets a valid escalation condition.

func TestRuleEntries(t *testing.T) {
	t.Parallel()

	logs := logging.NewLoggingWithFactory("rule-entries-test", zap.DebugLevel, time.Hour, testutils.NewTestLoggerFactory(t))
	runtimeConfig := NewRuntimeConfig(logs, nil)
	runtimeConfig.Rules = make(map[int64]*rule.Rule)
	for i := 1; i <= 50; i++ {
		runtimeConfig.Rules[int64(i)] = makeRule(t, i)
	}

	t.Run("Evaluate", func(t *testing.T) {
		t.Parallel()

		res := MakeResources(runtimeConfig, "test-evaluate-rule-entries")
		ruleEntries := make(RuleEntries)

		expectedLen := 0
		filterContext := &rule.EscalationFilter{IncidentSeverity: event.SeverityEmerg}
		assertEntries := func(rules map[int64]struct{}, expectedLen *int, expectError bool, opts EvalOptions) {
			if expectError {
				require.Error(t, ruleEntries.Evaluate(res, filterContext, rules, opts))
			} else {
				require.NoError(t, ruleEntries.Evaluate(res, filterContext, rules, opts))
			}
			require.Len(t, ruleEntries, *expectedLen)
			clear(ruleEntries) // Clear the entries for the next run.
		}
		expectedLen = len(runtimeConfig.Rules)/defaultDivisor - 5 // 15/3 => (5) valid entries are going to be deleted below.

		// Drop some random rules from the runtime config to simulate a runtime config deletion!
		maps.DeleteFunc(runtimeConfig.Rules, func(ruleID int64, _ *rule.Rule) bool { return ruleID > 35 && ruleID%defaultDivisor == 0 })

		opts := EvalOptions{
			OnPreEvaluate: func(re *rule.Entry) bool {
				if re.RuleID > 35 && re.RuleID%defaultDivisor == 0 { // Those rules are deleted from our runtime config.
					require.Failf(t, "OnPreEvaluate() shouldn't have been called", "rule %d was deleted from runtime config", re.RuleID)
				}
				require.Nilf(t, ruleEntries[re.ID], "Evaluate() shouldn't evaluate entry %d twice", re.ID)
				return true
			},
			OnError: func(re *rule.Entry, err error) bool {
				require.EqualError(t, err, `unknown severity "evaluable"`)
				return true
			},
			OnFilterMatch: func(re *rule.Entry) error {
				require.Nilf(t, ruleEntries[re.ID], "OnPreEvaluate() shouldn't evaluate %d twice", re.ID)
				return nil
			},
		}

		rules := make(map[int64]struct{}, len(runtimeConfig.Rules))
		for id := range runtimeConfig.Rules {
			rules[id] = struct{}{}
		}
		assertEntries(rules, &expectedLen, false, opts)

		lenBeforeError := new(int)
		opts.OnError = func(re *rule.Entry, err error) bool {
			if *lenBeforeError != 0 {
				require.Fail(t, "OnError() shouldn't have been called again")
			}
			require.EqualError(t, err, `unknown severity "evaluable"`)

			*lenBeforeError = len(ruleEntries)
			return false // This should let the evaluation fail completely!
		}
		assertEntries(rules, lenBeforeError, true, opts)

		*lenBeforeError = 0
		opts.OnError = nil
		opts.OnFilterMatch = func(re *rule.Entry) error {
			if *lenBeforeError != 0 {
				require.Fail(t, "OnFilterMatch() shouldn't have been called again")
			}
			*lenBeforeError = len(ruleEntries)
			return fmt.Errorf("OnFilterMatch() failed badly") // This should let the evaluation fail completely!
		}
		assertEntries(rules, lenBeforeError, true, opts)

		expectedLen = 0
		filterContext.IncidentSeverity = event.SeverityOK
		filterContext.IncidentAge = 5 * time.Minute

		opts.OnFilterMatch = nil
		opts.OnPreEvaluate = func(re *rule.Entry) bool { return re.RuleID < 5 }
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
	r.Entries = make(map[int64]*rule.Entry)

	invalidSeverity, err := filter.Parse("incident_severity=evaluable")
	require.NoError(t, err, "parsing incident_severity=evaluable shouldn't fail")

	redundant := new(rule.Entry)
	redundant.ID = r.ID * 150 // It must be large enough to avoid colliding with others!
	redundant.RuleID = r.ID
	redundant.Condition = invalidSeverity

	r.Entries[redundant.ID] = redundant
	if i%defaultDivisor == 0 {
		escalationCond, err := filter.Parse("incident_severity>warning||incident_age>=10m")
		require.NoError(t, err, "parsing incident_severity>warning||incident_age>=10m shouldn't fail")

		entry := new(rule.Entry)
		entry.ID = r.ID * 2
		entry.RuleID = r.ID
		entry.Condition = escalationCond

		r.Entries[entry.ID] = entry
	}

	return r
}
