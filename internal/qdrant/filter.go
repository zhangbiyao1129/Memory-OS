package qdrant

import (
	"errors"
	"strconv"
	"strings"
)

// FilterContext 是构造 Qdrant query-time payload filter 的权限上下文。
type FilterContext struct {
	UserID           string
	OrgID            string
	ProjectID        string
	Visibility       string
	PermissionLabels []string
	DocType          string
	IndexGeneration  int
}

// PayloadFilter 是 Phase 1 使用的中立 filter 表达，后续可转换成 Qdrant JSON。
type PayloadFilter struct {
	Must map[string][]string `json:"must"`
}

// BuildPayloadFilter 构造强制 query-time 过滤条件。
func BuildPayloadFilter(ctx FilterContext) (PayloadFilter, error) {
	if strings.TrimSpace(ctx.UserID) == "" {
		return PayloadFilter{}, errors.New("user_id is required for qdrant filter")
	}
	if strings.TrimSpace(ctx.Visibility) == "" {
		return PayloadFilter{}, errors.New("visibility is required for qdrant filter")
	}
	if ctx.Visibility != "private" && len(ctx.PermissionLabels) == 0 {
		return PayloadFilter{}, errors.New("permission_labels are required for non-private qdrant filter")
	}

	must := map[string][]string{
		"user_id":    {ctx.UserID},
		"visibility": {ctx.Visibility},
	}
	if ctx.OrgID != "" {
		must["org_id"] = []string{ctx.OrgID}
	}
	if ctx.ProjectID != "" {
		must["project_id"] = []string{ctx.ProjectID}
	}
	if len(ctx.PermissionLabels) > 0 {
		must["permission_labels"] = append([]string(nil), ctx.PermissionLabels...)
	}
	if ctx.DocType != "" {
		must["doc_type"] = []string{ctx.DocType}
	}
	if ctx.IndexGeneration > 0 {
		must["index_generation"] = []string{strconv.Itoa(ctx.IndexGeneration)}
	}

	return PayloadFilter{Must: must}, nil
}
