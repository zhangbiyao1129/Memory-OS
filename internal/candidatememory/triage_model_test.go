package candidatememory

import "testing"

func TestTriageScopeValid(t *testing.T) {
	scopes := []TriageScope{TriageScopeProject, TriageScopeGlobal, TriageScopeTooling, TriageScopePersonalPref, TriageScopeInbox, TriageScopeDiscard}
	for _, scope := range scopes {
		if !scope.Valid() {
			t.Fatalf("scope %q should be valid", scope)
		}
	}
	if TriageScope("unknown").Valid() {
		t.Fatal("unknown scope should be invalid")
	}
}

func TestGlobalHotMemoryProjectIDIsStable(t *testing.T) {
	if GlobalHotMemoryProjectID != "__global__" {
		t.Fatalf("GlobalHotMemoryProjectID = %q, want __global__", GlobalHotMemoryProjectID)
	}
}

func TestNormalizeReviewState(t *testing.T) {
	normalized := normalizeReviewState(TriageReviewNeedsReview)
	if normalized != TriageReviewNeedsReview {
		t.Fatalf("normalizeReviewState(need_review) = %q, want %q", normalized, TriageReviewNeedsReview)
	}

	if got := normalizeReviewState(TriageReviewState("\t" + string(TriageReviewWeak) + "\n")); got != TriageReviewWeak {
		t.Fatalf("normalizeReviewState() = %q, want %q", got, TriageReviewWeak)
	}
	if got := normalizeReviewState(TriageReviewState("unknown")); got != TriageReviewWeak {
		t.Fatalf("normalizeReviewState(unknown) = %q, want %q", got, TriageReviewWeak)
	}
}
