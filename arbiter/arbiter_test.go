package arbiter

import (
	"testing"
)

func TestIsIndependentPair(t *testing.T) {
	base := Review{TaskID: "t1", Generation: 1, Decision: DecisionApprove}

	primary := base
	primary.ReviewerID = "rev-a"
	primary.Role = RolePrimary

	secondary := base
	secondary.ReviewerID = "rev-b"
	secondary.Role = RoleSecondary

	if !IsIndependentPair(primary, secondary) {
		t.Fatal("distinct reviewers with distinct roles must be independent")
	}

	samePerson := secondary
	samePerson.ReviewerID = "rev-a"
	if IsIndependentPair(primary, samePerson) {
		t.Fatal("same reviewer must not be independent")
	}

	sameRole := secondary
	sameRole.Role = RolePrimary
	if IsIndependentPair(primary, sameRole) {
		t.Fatal("role overlap must not be independent")
	}

	otherGen := secondary
	otherGen.Generation = 2
	if IsIndependentPair(primary, otherGen) {
		t.Fatal("generation mismatch must not be independent")
	}
}
