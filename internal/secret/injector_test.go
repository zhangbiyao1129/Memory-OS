package secret

import (
	"bytes"
	"testing"

	"memory-os/internal/audit"
)

func TestInjectorReplacesSecretRefAndAudits(t *testing.T) {
	auditRepo := audit.NewMemoryRepository()
	vault := NewVault(NewMemoryRepository(), injectorCodec(t))
	meta, err := vault.Create(CreateRequest{OwnerUserID: "user_1", Name: "api", Plaintext: "fake-secret-value"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	injector := NewInjector(vault, audit.NewService(auditRepo))

	output, err := injector.Inject(InjectRequest{
		ActorUserID: "user_1",
		Tool:        "deploy",
		Purpose:     "integration-test",
		Target:      InjectionTargetEnv,
		RequestID:   "req_1",
		Template:    "TOKEN=${" + meta.SecretRef + "}",
	})

	if err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	if output != "TOKEN=fake-secret-value" {
		t.Fatalf("output = %q, want injected value", output)
	}
	if len(auditRepo.All()) != 1 {
		t.Fatalf("audit logs len = %d, want 1", len(auditRepo.All()))
	}
	if auditRepo.All()[0].Metadata["injection_target"] != string(InjectionTargetEnv) {
		t.Fatalf("audit injection_target = %q, want %q", auditRepo.All()[0].Metadata["injection_target"], InjectionTargetEnv)
	}
}

func TestInjectorRejectsLLMPromptTarget(t *testing.T) {
	auditRepo := audit.NewMemoryRepository()
	vault := NewVault(NewMemoryRepository(), injectorCodec(t))
	meta, err := vault.Create(CreateRequest{OwnerUserID: "user_1", Name: "api", Plaintext: "fake-secret-value"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	injector := NewInjector(vault, audit.NewService(auditRepo))

	_, err = injector.Inject(InjectRequest{
		ActorUserID: "user_1",
		Tool:        "llm",
		Purpose:     "prompt",
		Target:      InjectionTargetLLMPrompt,
		RequestID:   "req_1",
		Template:    "TOKEN=${" + meta.SecretRef + "}",
	})

	if err == nil {
		t.Fatal("Inject() error = nil, want llm prompt target rejection")
	}
	if len(auditRepo.All()) != 0 {
		t.Fatalf("audit logs len = %d, want 0 for rejected llm prompt injection", len(auditRepo.All()))
	}
}

func TestInjectorRejectsDisabledSecret(t *testing.T) {
	auditRepo := audit.NewMemoryRepository()
	vault := NewVault(NewMemoryRepository(), injectorCodec(t))
	meta, err := vault.Create(CreateRequest{OwnerUserID: "user_1", Name: "api", Plaintext: "fake-secret-value"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := vault.Disable(meta.SecretRef); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	injector := NewInjector(vault, audit.NewService(auditRepo))

	_, err = injector.Inject(InjectRequest{
		ActorUserID: "user_1",
		Tool:        "deploy",
		Purpose:     "integration-test",
		Target:      InjectionTargetEnv,
		RequestID:   "req_1",
		Template:    "TOKEN=${" + meta.SecretRef + "}",
	})

	if err == nil {
		t.Fatal("Inject() error = nil, want disabled secret rejection")
	}
}

func injectorCodec(t *testing.T) AESGCMCodec {
	t.Helper()

	codec, err := NewAESGCMCodec("key-1", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("NewAESGCMCodec() error = %v", err)
	}
	return codec
}
