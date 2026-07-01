package secret

import (
	"strings"

	"memory-os/internal/audit"
)

type InjectRequest struct {
	ActorUserID string
	OrgID       string
	ProjectID   string
	Tool        string
	Purpose     string
	RequestID   string
	Template    string
}

type Injector struct {
	vault Vault
	audit audit.Service
}

func NewInjector(vault Vault, auditService audit.Service) Injector {
	return Injector{vault: vault, audit: auditService}
}

func (i Injector) Inject(request InjectRequest) (string, error) {
	output := request.Template
	for _, ref := range extractRefs(request.Template) {
		value, err := i.vault.DecryptForUse(ref)
		if err != nil {
			return "", err
		}
		if err := i.audit.Record(audit.Log{
			ActorUserID:  request.ActorUserID,
			OrgID:        request.OrgID,
			ProjectID:    request.ProjectID,
			Action:       "secret.inject",
			ResourceType: "secret",
			ResourceID:   ref,
			RequestID:    request.RequestID,
			Result:       "ok",
			Metadata: map[string]string{
				"tool":    request.Tool,
				"purpose": request.Purpose,
			},
		}); err != nil {
			return "", err
		}
		output = strings.ReplaceAll(output, "${"+ref+"}", value)
	}
	return output, nil
}

func extractRefs(template string) []string {
	refs := []string{}
	remaining := template
	for {
		start := strings.Index(remaining, "${secret_ref_")
		if start < 0 {
			return refs
		}
		remaining = remaining[start+2:]
		end := strings.Index(remaining, "}")
		if end < 0 {
			return refs
		}
		refs = append(refs, remaining[:end])
		remaining = remaining[end+1:]
	}
}
