package playbooks

import "strings"

// NodeExpressPlaybook provides a minimal plan for simple Express /health scaffolding
type NodeExpressPlaybook struct{}

func (p NodeExpressPlaybook) Name() string { return "node_express_scaffold" }

// Deterministic scaffold; does not require broader workspace context
func (p NodeExpressPlaybook) RequiresContext() bool { return false }

func (p NodeExpressPlaybook) Matches(userIntent string, category string) bool {
	lo := strings.ToLower(userIntent)
	if strings.Contains(lo, "express") || strings.Contains(lo, "/health") {
		return true
	}
	return false
}

func (p NodeExpressPlaybook) BuildPlan(userIntent string, estimatedFiles []string) *PlanSpec {
	plan := &PlanSpec{Scope: "Create minimal Node Express API with GET /health -> {status:'ok'}"}
	plan.Files = append(plan.Files, "package.json", "server.js")
	plan.Ops = append(plan.Ops,
		PlanOp{
			FilePath:           "package.json",
			Description:        "Create minimal package.json with express dependency",
			Instructions:       "Create a package.json with name 'app', version '1.0.0', main 'server.js', and a start script 'node server.js'. Add express as a dependency. Keep strictly minimal.",
			ScopeJustification: "Required Node project manifest",
		},
		PlanOp{
			FilePath:           "server.js",
			Description:        "Create Express server with /health endpoint",
			Instructions:       "Create a minimal Express server: const express = require('express'); const app = express(); app.get('/health', (req,res)=>res.json({status:'ok'})); app.listen(3000);",
			ScopeJustification: "Implements requested endpoint",
		},
	)
	return plan
}

func init() { Register(NodeExpressPlaybook{}) }
