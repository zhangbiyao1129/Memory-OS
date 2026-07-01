package auth

import "testing"

func TestHashPasswordAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password hash equals plaintext")
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("VerifyPassword() = false, want true")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("VerifyPassword() = true for wrong password")
	}
}

func TestHashPasswordRejectsEmptyPassword(t *testing.T) {
	_, err := HashPassword("")
	if err == nil {
		t.Fatal("HashPassword() error = nil, want empty password error")
	}
}
