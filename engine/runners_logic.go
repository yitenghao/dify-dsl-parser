package engine

import (
	"context"
	"fmt"

	"dify-dsl-parser/dsl"
)

func init() {
	RegisterRunner(dsl.NodeTypeIfElse, runnerFunc(runIfElse))
}

// runIfElse evaluates each case in order; the first one to pass becomes the
// chosen edge. If none pass, the implicit "false" handle is used (the
// engine then routes to whichever edge has sourceHandle="false").
//
// Reference: graphon.nodes.if_else.if_else_node.IfElseNode._run
func runIfElse(_ context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.IfElseNodeData)
	if !ok {
		return nil, fmt.Errorf("if-else: unexpected data type %T", env.Node.Data)
	}

	usesLegacy := len(d.Cases) == 0
	cases := d.IterCases()

	process := []map[string]any{}
	selectedCaseID := "false"
	finalResult := false

	for _, c := range cases {
		ok, perCond, err := EvaluateConditions(env.Pool, c.Conditions, c.LogicalOperator)
		if err != nil {
			return &RunResult{
				Status: StatusFailed,
				Error:  err.Error(),
			}, nil
		}
		entry := map[string]any{
			"results":      perCond,
			"final_result": ok,
		}
		if usesLegacy {
			entry["group"] = "default"
		} else {
			entry["case_id"] = c.CaseID
		}
		process = append(process, entry)

		if ok {
			finalResult = true
			if usesLegacy {
				selectedCaseID = "true"
			} else {
				selectedCaseID = c.CaseID
			}
			break
		}
	}

	return &RunResult{
		Status:           StatusSucceeded,
		Inputs:           map[string]any{},
		ProcessData:      map[string]any{"condition_results": process},
		EdgeSourceHandle: selectedCaseID,
		Outputs: map[string]any{
			"result":           finalResult,
			"selected_case_id": selectedCaseID,
		},
	}, nil
}
