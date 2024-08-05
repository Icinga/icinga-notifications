package config

import (
	"github.com/icinga/icinga-notifications/internal/filter"
	"github.com/icinga/icinga-notifications/internal/rule"
)

// EvalOptions specifies optional callbacks that are executed upon certain filter evaluation events.
type EvalOptions[T, U any] struct {
	// OnPreEvaluate can be used to perform arbitrary actions before evaluating the current entry of type "T".
	// An entry of type "T" for which this hook returns "false" will be excluded from evaluation.
	OnPreEvaluate func(T) bool

	// OnError can be used to perform arbitrary actions on filter evaluation errors.
	// The original filter evaluation error is passed to this function as well as the current
	// entry of type "T", whose filter evaluation triggered the error.
	//
	// By default, the filter evaluation doesn't get interrupted if any of them fail, instead it will continue
	// evaluating all the remaining entries. However, you can override this behaviour by returning "false" in
	// your handler, in which case the filter evaluation is aborted prematurely.
	OnError func(T, error) bool

	// OnFilterMatch can be used to perform arbitrary actions after a successful filter evaluation of type "T".
	// This callback obtains the current entry of type "T" as an argument, whose filter matched on the filterableTest.
	//
	// Note, any error returned by the OnFilterMatch hook causes the filter evaluation to be aborted
	// immediately before even reaching the remaining ones.
	OnFilterMatch func(T) error

	// OnAllConfigEvaluated can be used to perform some post filter evaluation actions.
	// This handler receives an arbitrary value, be it a result of any filter evaluation or a made-up one of type "U".
	//
	// OnAllConfigEvaluated will only be called once all the entries of type "T" are evaluated, though it doesn't
	// necessarily depend on the result of the individual entry filter evaluation. If the individual Eval* receivers
	// don't return prematurely with an error, this hook is guaranteed to be called in any other cases. However, you
	// should be aware, that this hook may not be supported by all Eval* methods.
	OnAllConfigEvaluated func(U)
}

// Evaluable manages an evaluable config types in a centralised and structured way.
// An evaluable config is a config type that allows to evaluate filter expressions in some way.
type Evaluable struct {
	Rules       map[int64]bool        `db:"-"`
	RuleEntries map[int64]*rule.Entry `db:"-" json:"-"`
}

// NewEvaluable returns a fully initialised and ready to use Evaluable type.
func NewEvaluable() *Evaluable {
	return &Evaluable{
		Rules:       make(map[int64]bool),
		RuleEntries: make(map[int64]*rule.Entry),
	}
}

// EvaluateRules evaluates all the configured event rule.Rule(s) for the given filter.Filterable object.
//
// Please note that this function may not always evaluate *all* configured rules from the specified RuntimeConfig,
// as it internally caches all previously matched rules based on their ID.
//
// EvaluateRules allows you to specify EvalOptions and hook up certain filter evaluation steps.
// This function does not support the EvalOptions.OnAllConfigEvaluated callback and will never trigger
// it (if provided). Please refer to the description of the individual EvalOptions to find out more about
// when the hooks get triggered and possible special cases.
//
// Returns an error if any of the provided callbacks return an error, otherwise always nil.
func (e *Evaluable) EvaluateRules(r *RuntimeConfig, filterable filter.Filterable, options EvalOptions[*rule.Rule, any]) error {
	for _, ru := range r.Rules {
		if !e.Rules[ru.ID] && (options.OnPreEvaluate == nil || options.OnPreEvaluate(ru)) {
			matched, err := ru.Eval(filterable)
			if err != nil && options.OnError != nil && !options.OnError(ru, err) {
				return err
			}
			if err != nil || !matched {
				continue
			}

			if options.OnFilterMatch != nil {
				if err := options.OnFilterMatch(ru); err != nil {
					return err
				}
			}

			e.Rules[ru.ID] = true
		}
	}

	return nil
}

// EvaluateRuleEntries evaluates all the configured rule.Entry for the provided filter.Filterable object.
//
// This function allows you to specify EvalOptions and hook up certain filter evaluation steps.
// Currently, EvaluateRuleEntries fully support all the available EvalOptions. Please refer to the
// description of the individual EvalOptions to find out more about when the hooks get triggered and
// possible special cases.
//
// Returns an error if any of the provided callbacks return an error, otherwise always nil.
func (e *Evaluable) EvaluateRuleEntries(r *RuntimeConfig, filterable filter.Filterable, options EvalOptions[*rule.Entry, any]) error {
	retryAfter := rule.RetryNever

	for ruleID := range e.Rules {
		ru := r.Rules[ruleID]
		if ru == nil {
			// It would be appropriate to have a debug log here, but unfortunately we don't have access to a logger.
			continue
		}

		for _, entry := range ru.Entries {
			if options.OnPreEvaluate != nil && !options.OnPreEvaluate(entry) {
				continue
			}

			if matched, err := entry.Eval(filterable); err != nil {
				if options.OnError != nil && !options.OnError(entry, err) {
					return err
				}
			} else if cond, ok := filterable.(*rule.EscalationFilter); !matched && ok {
				incidentAgeFilter := cond.ReevaluateAfter(entry.Condition)
				retryAfter = min(retryAfter, incidentAgeFilter)
			} else if matched {
				if options.OnFilterMatch != nil {
					if err := options.OnFilterMatch(entry); err != nil {
						return err
					}
				}

				e.RuleEntries[entry.ID] = entry
			}
		}
	}

	if options.OnAllConfigEvaluated != nil {
		options.OnAllConfigEvaluated(retryAfter)
	}

	return nil
}
