package configuration

import "testing"

func TestIsBuildTestCmd(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		// npm/yarn/pnpm safe subcommands
		{"npm test", true}, {"npm run build", true}, {"npm run lint", true},
		{"yarn test", true}, {"pnpm build", true},
		{"npm install", false}, {"npm publish", false}, {"npm add", false},
		{"npm uninstall", false}, {"npm", false},
		// make
		{"make build-all", true}, {"make", true},
		// go
		{"go test ./...", true}, {"go build ./...", true}, {"go vet", true},
		{"go install", false}, {"go get", false},
		// other build tools
		{"cargo build", true}, {"cargo test", true}, {"mvn clean install", true},
		{"gradle build", true}, {"dotnet build", true},
		// script runners
		{"node script.js", true}, {"python3 -m pytest", true},
		{"ruby run.rb", true}, {"deno run main.ts", true},
		// kubectl safe
		{"kubectl get pods", true}, {"kubectl describe pod x", true},
		{"kubectl logs app", true},
		{"kubectl apply -f x.yaml", false}, {"kubectl delete pod x", false},
		// terraform safe
		{"terraform plan", true}, {"terraform validate", true},
		{"terraform apply", false}, {"terraform destroy", false},
		// docker safe
		{"docker compose up", true}, {"docker ps", true}, {"docker logs c", true},
		{"docker run --rm alpine", false},
		// negative cases — destructive ops must fall through
		{"rm -rf /", false}, {"git status", false}, {"", false},
	}
	for _, c := range cases {
		got := isBuildTestCmd(c.cmd)
		if got != c.want {
			t.Errorf("isBuildTestCmd(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestCategorizeCommandBuildTest(t *testing.T) {
	cases := []struct {
		cmd string
		cat string
	}{
		{"npm test", "build_test"},
		{"make build-all", "build_test"},
		{"go test ./...", "build_test"},
		{"cargo build", "build_test"},
		// install/mutating should NOT be build_test
		{"npm install", "shell_command"},
		{"go install", "shell_command"},
		// destructive stays critical-ish
		{"rm -rf /", "rm_command"},
	}
	for _, c := range cases {
		got := categorizeCommand(c.cmd)
		if got != c.cat {
			t.Errorf("categorizeCommand(%q) = %q, want %q", c.cmd, got, c.cat)
		}
	}
}

func TestBuildTestResolvesLowOnDefault(t *testing.T) {
	rules := AutoApproveRulesForProfile(RiskProfileDefault)
	st := &SubagentType{AutoApproveRules: &rules}
	commands := []string{
		"npm test", "npm run build", "make build-all",
		"go test ./...", "go build ./...",
		"cargo test", "node script.js",
	}
	for _, cmd := range commands {
		risk := st.EvaluateOperationRisk(cmd)
		if risk != RiskLevelLow {
			t.Errorf("EvaluateOperationRisk(%q) = %v, want Low", cmd, risk)
		}
	}
}

func TestBuildTestMutatingStaysMediumOnDefault(t *testing.T) {
	rules := AutoApproveRulesForProfile(RiskProfileDefault)
	st := &SubagentType{AutoApproveRules: &rules}
	commands := []string{
		"npm install", "npm publish", "npm add lodash",
		"go install", "go get example.com/pkg",
		"terraform apply", "kubectl apply -f x.yaml",
	}
	for _, cmd := range commands {
		risk := st.EvaluateOperationRisk(cmd)
		if risk != RiskLevelMedium {
			t.Errorf("EvaluateOperationRisk(%q) = %v, want Medium", cmd, risk)
		}
	}
}
