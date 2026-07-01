package health

import "context"

const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

// Checker 是外部组件健康检查的最小接口。
type Checker interface {
	Check(ctx context.Context) error
}

// Component 描述单个组件的健康状态。
type Component struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Report 描述系统整体健康状态。
type Report struct {
	Status     string               `json:"status"`
	Components map[string]Component `json:"components"`
}

// Service 聚合 API、DB、Redis、Qdrant 等组件健康状态。
type Service struct {
	checkers map[string]Checker
}

func NewService(checkers map[string]Checker) Service {
	copied := make(map[string]Checker, len(checkers))
	for name, checker := range checkers {
		if checker != nil {
			copied[name] = checker
		}
	}
	return Service{checkers: copied}
}

func (s Service) Check(ctx context.Context) Report {
	report := Report{
		Status:     StatusOK,
		Components: make(map[string]Component, len(s.checkers)),
	}

	for name, checker := range s.checkers {
		if err := checker.Check(ctx); err != nil {
			report.Status = StatusDegraded
			report.Components[name] = Component{Status: StatusDegraded, Error: err.Error()}
			continue
		}
		report.Components[name] = Component{Status: StatusOK}
	}

	return report
}
