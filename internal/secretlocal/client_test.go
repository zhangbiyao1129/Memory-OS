package secretlocal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"memory-os/internal/secret"
)

func TestClientCreateSendsCiphertextNeverPlaintext(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/memory/secrets/create" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer token: %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "fake-secret-value") {
			t.Fatalf("request body leaked plaintext: %s", body)
		}
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"secret_ref":"secret_ref_1","name":"api-key","status":"active"}`))
	}))
	defer server.Close()

	key, _ := GenerateDeviceKey("device-1")
	client := NewClient(server.URL, "test-token", server.Client())
	blob, err := key.Encrypt([]byte("fake-secret-value"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	meta, err := client.Create(CreateParams{OrgID: "org_1", ProjectID: "project_1", Name: "api-key"}, blob)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if meta.SecretRef != "secret_ref_1" {
		t.Fatalf("secret_ref = %q", meta.SecretRef)
	}
	if _, ok := captured["plaintext"]; ok {
		t.Fatal("request contained plaintext field")
	}
	enc, ok := captured["encrypted"].(map[string]any)
	if !ok {
		t.Fatalf("request missing encrypted blob: %v", captured)
	}
	if enc["ciphertext_b64"] == "" || enc["nonce_b64"] == "" {
		t.Fatalf("encrypted blob incomplete: %v", enc)
	}
}

func TestClientGetCiphertextParsesBlob(t *testing.T) {
	key, _ := GenerateDeviceKey("device-1")
	blob, _ := key.Encrypt([]byte("fake-secret-value"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/memory/secrets/ciphertext" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		response := map[string]any{
			"secret_ref": "secret_ref_1",
			"encrypted": map[string]any{
				"algorithm":       blob.Algorithm,
				"device_key_id":   blob.DeviceKeyID,
				"key_fingerprint": blob.KeyFingerprint,
				"nonce_b64":       b64(blob.Nonce),
				"ciphertext_b64":  b64(blob.Ciphertext),
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", server.Client())
	_, got, err := client.GetCiphertext("secret_ref_1")
	if err != nil {
		t.Fatalf("GetCiphertext() error = %v", err)
	}
	plaintext, err := key.Decrypt(got)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(plaintext) != "fake-secret-value" {
		t.Fatalf("decrypted = %q", plaintext)
	}
}

func TestClientPropagatesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"secret_forbidden"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", server.Client())
	if _, _, err := client.GetCiphertext("secret_ref_1"); err == nil {
		t.Fatal("GetCiphertext() error = nil, want forbidden error")
	}
}

func b64(data []byte) string {
	return encodeStd(data)
}

var _ = secret.EncryptedBlob{}
