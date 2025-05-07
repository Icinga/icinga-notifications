package config

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/rule"
	"github.com/stretchr/testify/require"
	"maps"
	"testing"
	"time"
)

const defaultDivisor = 3

func TestEvaluableConfig(t *testing.T) {
	t.Parallel()

	runtimeConfigTest := new(RuntimeConfig)
	runtimeConfigTest.Rules = make(map[int64]*rule.Rule)
	for i := 1; i <= 50; i++ {
		runtimeConfigTest.Rules[int64(i)] = makeRule(t, i)
	}

	t.Run("NewEvaluable", func(t *testing.T) {
		t.Parallel()

		e := NewEvaluable()
		require.NotNil(t, e, "it should create a fully initialised evaluable config")
		require.NotNil(t, e.Rules)
		require.NotNil(t, e.RuleEntries)
	})

	t.Run("EvaluateRules", func(t *testing.T) {
		t.Parallel()

		runtime := new(RuntimeConfig)
		runtime.Rules = maps.Clone(runtimeConfigTest.Rules)

		expectedLen := len(runtime.Rules) / defaultDivisor
		options := EvalOptions[*rule.Rule, any]{}
		e := NewEvaluable()
		assertRules := func(expectedLen *int, expectError bool) {
			if expectError {
				require.Error(t, e.EvaluateRules(runtime, new(filterableTest), options))
			} else {
				require.NoError(t, e.EvaluateRules(runtime, new(filterableTest), options))
			}
			require.Len(t, e.Rules, *expectedLen)
		}

		assertRules(&expectedLen, false)
		maps.DeleteFunc(e.Rules, func(ruleID int64, _ bool) bool { return int(ruleID) > expectedLen/2 })

		options.OnPreEvaluate = func(r *rule.Rule) bool {
			require.Falsef(t, e.Rules[r.ID], "EvaluateRules() shouldn't evaluate %q twice", r.Name)
			return true
		}
		options.OnError = func(r *rule.Rule, err error) bool {
			require.EqualError(t, err, `"nonexistent" is not a valid filter key`)
			require.Truef(t, r.ID%defaultDivisor != 0, "evaluating rule %q should not fail", r.Name)
			return true
		}
		options.OnFilterMatch = func(r *rule.Rule) error {
			require.Falsef(t, e.Rules[r.ID], "EvaluateRules() shouldn't evaluate %q twice", r.Name)
			return nil
		}

		assertRules(&expectedLen, false)
		maps.DeleteFunc(e.Rules, func(ruleID int64, _ bool) bool { return int(ruleID) > expectedLen/2 })

		lenBeforeError := new(int)
		options.OnError = func(r *rule.Rule, err error) bool {
			if *lenBeforeError != 0 {
				require.Fail(t, "OnError() shouldn't have been called again")
			}

			require.EqualError(t, err, `"nonexistent" is not a valid filter key`)
			require.Truef(t, r.ID%defaultDivisor != 0, "evaluating rule %q should not fail", r.Name)

			*lenBeforeError = len(e.Rules)
			return false // This should let the evaluation fail completely!
		}
		assertRules(lenBeforeError, true)
		maps.DeleteFunc(e.Rules, func(ruleID int64, _ bool) bool { return int(ruleID) > expectedLen/2 })

		*lenBeforeError = 0
		options.OnError = nil
		options.OnFilterMatch = func(r *rule.Rule) error {
			if *lenBeforeError != 0 {
				require.Fail(t, "OnFilterMatch() shouldn't have been called again")
			}

			*lenBeforeError = len(e.Rules)
			return fmt.Errorf("OnFilterMatch() failed badly") // This should let the evaluation fail completely!
		}
		assertRules(lenBeforeError, true)
	})

	t.Run("EvaluateRuleEntries", func(t *testing.T) {
		t.Parallel()

		runtime := new(RuntimeConfig)
		runtime.Rules = maps.Clone(runtimeConfigTest.Rules)

		e := NewEvaluable()
		options := EvalOptions[*rule.Escalation, any]{}

		expectedLen := 0
		filterContext := &rule.EscalationFilter{IncidentSeverity: 9} // Event severity "emergency"
		assertEntries := func(expectedLen *int, expectError bool) {
			if expectError {
				require.Error(t, e.EvaluateRuleEntries(runtime, filterContext, options))
			} else {
				require.NoError(t, e.EvaluateRuleEntries(runtime, filterContext, options))
			}
			require.Len(t, e.RuleEntries, *expectedLen)
			e.RuleEntries = make(map[int64]*rule.Escalation)
		}

		assertEntries(&expectedLen, false)
		require.NoError(t, e.EvaluateRules(runtime, new(filterableTest), EvalOptions[*rule.Rule, any]{}))
		require.Len(t, e.Rules, len(runtime.Rules)/defaultDivisor)
		expectedLen = len(runtime.Rules)/defaultDivisor - 5 // 15/3 => (5) valid entries are going to be deleted below.

		// Drop some random rules from the runtime config to simulate a runtime config deletion!
		maps.DeleteFunc(runtime.Rules, func(ruleID int64, _ *rule.Rule) bool { return ruleID > 35 && ruleID%defaultDivisor == 0 })

		options.OnPreEvaluate = func(re *rule.Escalation) bool {
			if re.RuleID > 35 && re.RuleID%defaultDivisor == 0 { // Those rules are deleted from our runtime config.
				require.Failf(t, "OnPreEvaluate() shouldn't have been called", "rule %d was deleted from runtime config", re.RuleID)
			}

			require.Nilf(t, e.RuleEntries[re.ID], "EvaluateRuleEntries() shouldn't evaluate entry %d twice", re.ID)
			return true
		}
		options.OnError = func(re *rule.Escalation, err error) bool {
			require.EqualError(t, err, `unknown severity "evaluable"`)
			require.Truef(t, re.RuleID%defaultDivisor == 0, "evaluating rule entry %d should not fail", re.ID)
			return true
		}
		options.OnFilterMatch = func(re *rule.Escalation) error {
			require.Nilf(t, e.RuleEntries[re.ID], "OnPreEvaluate() shouldn't evaluate %d twice", re.ID)
			return nil
		}
		assertEntries(&expectedLen, false)

		lenBeforeError := new(int)
		options.OnError = func(re *rule.Escalation, err error) bool {
			if *lenBeforeError != 0 {
				require.Fail(t, "OnError() shouldn't have been called again")
			}

			require.EqualError(t, err, `unknown severity "evaluable"`)
			require.Truef(t, re.RuleID%defaultDivisor == 0, "evaluating rule entry %d should not fail", re.ID)

			*lenBeforeError = len(e.RuleEntries)
			return false // This should let the evaluation fail completely!
		}
		assertEntries(lenBeforeError, true)

		*lenBeforeError = 0
		options.OnError = nil
		options.OnFilterMatch = func(re *rule.Escalation) error {
			if *lenBeforeError != 0 {
				require.Fail(t, "OnFilterMatch() shouldn't have been called again")
			}

			*lenBeforeError = len(e.RuleEntries)
			return fmt.Errorf("OnFilterMatch() failed badly") // This should let the evaluation fail completely!
		}
		assertEntries(lenBeforeError, true)

		expectedLen = 0
		filterContext.IncidentSeverity = 1 // OK
		filterContext.IncidentAge = 5 * time.Minute

		options.OnFilterMatch = nil
		options.OnPreEvaluate = func(re *rule.Escalation) bool { return re.RuleID < 5 }
		options.OnAllConfigEvaluated = func(result any) {
			retryAfter := result.(time.Duration)
			// The filter string of the escalation condition is incident_age>=10m and the actual incident age is 5m.
			require.Equal(t, 5*time.Minute, retryAfter)
		}
		assertEntries(&expectedLen, false)
	})
}

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

	nonexistent, err := filter.Parse("nonexistent=evaluable")
	require.NoError(t, err, "parsing nonexistent=evaluable shouldn't fail")

	r.Escalations[redundant.ID] = redundant
	r.ObjectFilter = nonexistent
	if i%defaultDivisor == 0 {
		objCond, err := filter.Parse("host=evaluable")
		require.NoError(t, err, "parsing host=evaluable shouldn't fail")

		escalationCond, err := filter.Parse("incident_severity>warning||incident_age>=10m")
		require.NoError(t, err, "parsing incident_severity>=ok shouldn't fail")

		entry := new(rule.Escalation)
		entry.ID = r.ID * 2
		entry.RuleID = r.ID
		entry.Condition = escalationCond

		r.ObjectFilter = objCond
		r.Escalations[entry.ID] = entry
	}

	return r
}

// filterableTest is a test type that simulates a filter evaluation and eliminates
// the need of having to import e.g. the object package.
type filterableTest struct{}

func (f *filterableTest) EvalEqual(k string, v string) (bool, error) {
	if k != "host" {
		return false, fmt.Errorf("%q is not a valid filter key", k)
	}

	return v == "evaluable", nil
}

func (f *filterableTest) EvalExists(_ string) bool { return true }
func (f *filterableTest) EvalLess(_ string, _ string) (bool, error) {
	panic("Oh dear - you shouldn't have called me")
}
func (f *filterableTest) EvalLike(_, _ string) (bool, error)        { return f.EvalLess("", "") }
func (f *filterableTest) EvalLessOrEqual(_, _ string) (bool, error) { return f.EvalLess("", "") }
