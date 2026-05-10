package core

import "testing"

func TestPolicyAuthorize(t *testing.T) {
	policy := Policy{
		AuthorizedUsers: []string{"ou_allowed"},
		AllowedRepos:    []string{"/tmp/work"},
	}

	if err := policy.Authorize("ou_allowed", "/tmp/work/repo"); err != nil {
		t.Fatal(err)
	}
	if err := policy.Authorize("ou_other", "/tmp/work/repo"); err == nil {
		t.Fatal("expected unauthorized user error")
	}
	if err := policy.Authorize("ou_allowed", "/tmp/other"); err == nil {
		t.Fatal("expected disallowed repo error")
	}
}

func TestAssessCommandRisk(t *testing.T) {
	risk := AssessCommandRisk("git reset --hard HEAD")
	if risk.Level != "high" || risk.SessionGrant {
		t.Fatalf("risk = %#v", risk)
	}

	risk = AssessCommandRisk("npm install")
	if risk.Level != "medium" || !risk.SessionGrant {
		t.Fatalf("risk = %#v", risk)
	}
}
