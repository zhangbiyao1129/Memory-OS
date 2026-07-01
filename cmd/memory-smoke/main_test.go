package main

import "testing"

func TestAssertNoSecretLeakRejectsSensitivePayloads(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{name: "api key", payload: `{"context":"use sk-test-redacted-example"}`},
		{name: "password assignment", payload: `{"context":"password = password-test-redacted"}`},
		{name: "private key", payload: "-----BEGIN PRIVATE KEY-----\nfake-test-redacted\n-----END PRIVATE KEY-----"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := assertNoSecretLeak("test payload", tc.payload); err == nil {
				t.Fatal("assertNoSecretLeak() error = nil, want secret leak rejection")
			}
		})
	}
}

func TestAssertNoSecretLeakAllowsSecretRefs(t *testing.T) {
	payload := `{"context":"token stored as secret_ref:secret_1"}`

	if err := assertNoSecretLeak("test payload", payload); err != nil {
		t.Fatalf("assertNoSecretLeak() error = %v, want nil", err)
	}
}
