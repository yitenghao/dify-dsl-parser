package engine

import (
	"fmt"
	"strconv"
	"strings"

	"dify-dsl-parser/dsl"
)

// EvaluateConditions runs every condition in conditions and combines the
// results with the given logical operator ("and" / "or").
//
// Reference: graphon.utils.condition.processor.ConditionProcessor.process_conditions
func EvaluateConditions(
	pool *VariablePool,
	conditions []dsl.Condition,
	operator string,
) (final bool, perCond []bool, err error) {
	if len(conditions) == 0 {
		return false, nil, nil
	}
	op := strings.ToLower(operator)
	if op == "" {
		op = "and"
	}

	results := make([]bool, 0, len(conditions))
	for _, c := range conditions {
		actual, found := pool.Get(c.VariableSelector)
		ok, err := evaluateOne(actual, found, c.ComparisonOperator, c.Value)
		if err != nil {
			return false, results, fmt.Errorf("selector=%v op=%s: %w",
				c.VariableSelector, c.ComparisonOperator, err)
		}
		results = append(results, ok)
	}

	switch op {
	case "or":
		final = false
		for _, r := range results {
			if r {
				final = true
				break
			}
		}
	default: // "and"
		final = true
		for _, r := range results {
			if !r {
				final = false
				break
			}
		}
	}
	return final, results, nil
}

// evaluateOne evaluates a single comparison operator. found indicates whether
// the variable existed in the pool (used for null / not-null / exists / not-exists).
func evaluateOne(actual any, found bool, operator string, expected any) (bool, error) {
	switch operator {
	// Existence
	case "null", "not exists":
		return !found || actual == nil, nil
	case "not null", "exists":
		return found && actual != nil, nil
	case "empty":
		return isEmpty(actual), nil
	case "not empty":
		return !isEmpty(actual), nil
	}

	// String / array operators that work on the textual form
	a := stringify(actual)
	e := stringify(expected)

	switch operator {
	case "is":
		return a == e, nil
	case "is not":
		return a != e, nil
	case "contains":
		return strings.Contains(a, e), nil
	case "not contains":
		return !strings.Contains(a, e), nil
	case "start with":
		return strings.HasPrefix(a, e), nil
	case "end with":
		return strings.HasSuffix(a, e), nil
	case "in":
		return inSlice(actual, expected), nil
	case "not in":
		return !inSlice(actual, expected), nil
	case "all of":
		return allOf(actual, expected), nil
	}

	// Numeric comparisons
	switch operator {
	case "=", "≠", ">", "<", "≥", "≤":
		af, aok := toFloat(actual)
		ef, eok := toFloat(expected)
		if !aok || !eok {
			return false, fmt.Errorf("non-numeric value for %q (lhs=%v rhs=%v)", operator, actual, expected)
		}
		switch operator {
		case "=":
			return af == ef, nil
		case "≠":
			return af != ef, nil
		case ">":
			return af > ef, nil
		case "<":
			return af < ef, nil
		case "≥":
			return af >= ef, nil
		case "≤":
			return af <= ef, nil
		}
	}

	return false, fmt.Errorf("unsupported comparison operator %q", operator)
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func inSlice(needle, haystack any) bool {
	n := stringify(needle)
	switch xs := haystack.(type) {
	case []any:
		for _, e := range xs {
			if stringify(e) == n {
				return true
			}
		}
	case []string:
		for _, e := range xs {
			if e == n {
				return true
			}
		}
	}
	return false
}

func allOf(haystack, needles any) bool {
	// "all of": every element of needles is contained in haystack.
	hs, ok := toStringSlice(haystack)
	if !ok {
		return false
	}
	ns, ok := toStringSlice(needles)
	if !ok {
		return false
	}
	hset := make(map[string]struct{}, len(hs))
	for _, e := range hs {
		hset[e] = struct{}{}
	}
	for _, e := range ns {
		if _, ok := hset[e]; !ok {
			return false
		}
	}
	return true
}

func toStringSlice(v any) ([]string, bool) {
	switch xs := v.(type) {
	case []any:
		out := make([]string, len(xs))
		for i, e := range xs {
			out[i] = stringify(e)
		}
		return out, true
	case []string:
		return xs, true
	case string:
		return []string{xs}, true
	}
	return nil, false
}
