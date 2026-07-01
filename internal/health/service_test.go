package health

import (
	"context"
	"errors"
	"testing"
)

type probeFunc func(context.Context) error

func (p probeFunc) Check(ctx context.Context) error {
	return p(ctx)
}

func TestServiceReportsHealthyWhenAllComponentsPass(t *testing.T) {
	service := NewService(map[string]Checker{
		"api":   probeFunc(func(context.Context) error { return nil }),
		"redis": probeFunc(func(context.Context) error { return nil }),
	})

	report := service.Check(context.Background())

	if report.Status != StatusOK {
		t.Fatalf("status = %q, want %q", report.Status, StatusOK)
	}
	if report.Components["api"].Status != StatusOK {
		t.Fatalf("api status = %q, want %q", report.Components["api"].Status, StatusOK)
	}
}

func TestServiceReportsDegradedWhenAComponentFails(t *testing.T) {
	service := NewService(map[string]Checker{
		"api": probeFunc(func(context.Context) error { return nil }),
		"db":  probeFunc(func(context.Context) error { return errors.New("connection refused") }),
	})

	report := service.Check(context.Background())

	if report.Status != StatusDegraded {
		t.Fatalf("status = %q, want %q", report.Status, StatusDegraded)
	}
	if report.Components["db"].Status != StatusDegraded {
		t.Fatalf("db status = %q, want %q", report.Components["db"].Status, StatusDegraded)
	}
	if report.Components["db"].Error == "" {
		t.Fatal("db error is empty, want sanitized error summary")
	}
}

func TestServiceHandlesNoComponents(t *testing.T) {
	service := NewService(nil)

	report := service.Check(context.Background())

	if report.Status != StatusOK {
		t.Fatalf("status = %q, want %q", report.Status, StatusOK)
	}
	if len(report.Components) != 0 {
		t.Fatalf("components len = %d, want 0", len(report.Components))
	}
}
