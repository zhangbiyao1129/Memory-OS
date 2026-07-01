package qdrant

import "testing"

func TestBuildPayloadFilterRequiresUserID(t *testing.T) {
	_, err := BuildPayloadFilter(FilterContext{
		OrgID:     "org_alpha",
		ProjectID: "project_alpha",
	})

	if err == nil {
		t.Fatal("BuildPayloadFilter() error = nil, want missing user id error")
	}
}

func TestBuildPayloadFilterRequiresVisibility(t *testing.T) {
	_, err := BuildPayloadFilter(FilterContext{
		UserID:    "user_alice",
		OrgID:     "org_alpha",
		ProjectID: "project_alpha",
	})

	if err == nil {
		t.Fatal("BuildPayloadFilter() error = nil, want missing visibility error")
	}
}

func TestBuildPayloadFilterIncludesTenantAndPermissionLabels(t *testing.T) {
	filter, err := BuildPayloadFilter(FilterContext{
		UserID:           "user_alice",
		OrgID:            "org_alpha",
		ProjectID:        "project_alpha",
		Visibility:       "project",
		PermissionLabels: []string{"project:project_alpha:read", "org:org_alpha:member"},
		DocType:          "archive_chunk",
		IndexGeneration:  2,
	})
	if err != nil {
		t.Fatalf("BuildPayloadFilter() error = %v", err)
	}

	wantKeys := []string{"user_id", "org_id", "project_id", "visibility", "permission_labels", "doc_type", "index_generation"}
	for _, key := range wantKeys {
		if _, ok := filter.Must[key]; !ok {
			t.Fatalf("filter missing key %q: %#v", key, filter.Must)
		}
	}
	if len(filter.Must["permission_labels"]) != 2 {
		t.Fatalf("permission labels len = %d, want 2", len(filter.Must["permission_labels"]))
	}
}

func TestBuildPayloadFilterRejectsEmptyPermissionLabelForProjectScope(t *testing.T) {
	_, err := BuildPayloadFilter(FilterContext{
		UserID:     "user_alice",
		OrgID:      "org_alpha",
		ProjectID:  "project_alpha",
		Visibility: "project",
	})

	if err == nil {
		t.Fatal("BuildPayloadFilter() error = nil, want missing permission labels error")
	}
}
