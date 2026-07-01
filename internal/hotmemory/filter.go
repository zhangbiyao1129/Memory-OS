package hotmemory

import (
	"errors"
	"strings"

	"memory-os/internal/qdrant"
)

type PayloadFilter = qdrant.PayloadFilter

type FilterContext struct {
	OrgID            string
	ProjectID        string
	UserID           string
	AgentID          string
	Scope            Scope
	Visibility       string
	PermissionLabels []string
}

func BuildFilter(ctx FilterContext) (PayloadFilter, error) {
	if strings.TrimSpace(ctx.UserID) == "" {
		return PayloadFilter{}, errors.New("user_id is required")
	}
	if strings.TrimSpace(string(ctx.Scope)) == "" {
		return PayloadFilter{}, errors.New("scope is required")
	}
	if strings.TrimSpace(ctx.Visibility) == "" {
		return PayloadFilter{}, errors.New("visibility is required")
	}
	if ctx.Visibility != "private" && len(ctx.PermissionLabels) == 0 {
		return PayloadFilter{}, errors.New("permission labels are required")
	}
	if ctx.Scope == ScopeAgentSpecific && strings.TrimSpace(ctx.AgentID) == "" {
		return PayloadFilter{}, errors.New("agent_id is required for agent_specific scope")
	}
	must := map[string][]string{
		"doc_type":   {"hot_memory"},
		"user_id":    {ctx.UserID},
		"scope":      {string(ctx.Scope)},
		"visibility": {ctx.Visibility},
	}
	if ctx.OrgID != "" {
		must["org_id"] = []string{ctx.OrgID}
	}
	if ctx.ProjectID != "" {
		must["project_id"] = []string{ctx.ProjectID}
	}
	if ctx.AgentID != "" && ctx.Scope == ScopeAgentSpecific {
		must["agent_id"] = []string{ctx.AgentID}
	}
	if len(ctx.PermissionLabels) > 0 {
		must["permission_labels"] = append([]string(nil), ctx.PermissionLabels...)
	}
	return PayloadFilter{Must: must}, nil
}

func matchesFilter(memory Memory, filter map[string][]string) bool {
	for key, allowed := range filter {
		if key == "permission_labels" {
			if !hasAny(memory.PermissionLabels, allowed) {
				return false
			}
			continue
		}
		value := memoryPayloadValue(memory, key)
		if value == "" || !contains(allowed, value) {
			return false
		}
	}
	return true
}

func memoryPayloadValue(memory Memory, key string) string {
	switch key {
	case "doc_type":
		return "hot_memory"
	case "org_id":
		return memory.OrgID
	case "project_id":
		return memory.ProjectID
	case "user_id":
		return memory.UserID
	case "agent_id":
		return memory.AgentID
	case "scope":
		return string(memory.Scope)
	case "visibility":
		return memory.Visibility
	default:
		return ""
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasAny(values []string, targets []string) bool {
	for _, target := range targets {
		if contains(values, target) {
			return true
		}
	}
	return false
}
