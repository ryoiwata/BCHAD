package plan

import (
	"fmt"

	"github.com/athena-digital/bchad/internal/gateway"
	"github.com/athena-digital/bchad/pkg/bchadplan"
)

// stageProfile holds the token and retry estimates for a single stage type.
type stageProfile struct {
	Model        string
	AvgInputTok  int
	AvgOutputTok int
	AvgAttempts  float64
}

// stageProfiles is the cost model from the BCHAD framework document.
var stageProfiles = map[string]stageProfile{
	"migrate": {
		Model:        modelHaiku,
		AvgInputTok:  25_000,
		AvgOutputTok: 5_000,
		AvgAttempts:  1.1,
	},
	"config": {
		Model:        modelHaiku,
		AvgInputTok:  15_000,
		AvgOutputTok: 3_000,
		AvgAttempts:  1.0,
	},
	"api": {
		Model:        modelSonnet,
		AvgInputTok:  60_000,
		AvgOutputTok: 15_000,
		AvgAttempts:  1.3,
	},
	"frontend": {
		Model:        modelSonnet,
		AvgInputTok:  70_000,
		AvgOutputTok: 20_000,
		AvgAttempts:  1.2,
	},
	"tests": {
		Model:        modelSonnet,
		AvgInputTok:  50_000,
		AvgOutputTok: 15_000,
		AvgAttempts:  1.2,
	},
}

// EstimateStageCost computes the projected USD cost for a single stage,
// including the expected retry rate from the framework cost model.
func EstimateStageCost(stageID string) (float64, error) {
	profile, ok := stageProfiles[stageID]
	if !ok {
		return 0, fmt.Errorf("unknown stage %q: no cost profile available", stageID)
	}

	usage := gateway.Usage{
		InputTokens:  profile.AvgInputTok,
		OutputTokens: profile.AvgOutputTok,
	}
	perAttemptCost := gateway.ComputeCost(profile.Model, usage)
	return perAttemptCost * profile.AvgAttempts, nil
}

// EstimatePlanCost computes the total projected cost for a BCHADPlan
// as the sum of all stage costs. If any stage cost cannot be computed,
// it is skipped and the stage's EstimatedCost remains 0.
func EstimatePlanCost(plan *bchadplan.BCHADPlan) float64 {
	var total float64
	for i, stage := range plan.Stages {
		cost, err := EstimateStageCost(stage.ID)
		if err == nil {
			plan.Stages[i].EstimatedCost = cost
			total += cost
		}
	}
	return total
}
