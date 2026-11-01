package validation

import "testing"

func TestNormalizeEmail(t *testing.T) {
	got, ok := NormalizeEmail("  Owner@Example.com ")
	if !ok || got != "owner@example.com" {
		t.Fatalf("NormalizeEmail result = %q, %v", got, ok)
	}
	for _, email := range []string{"", "not-an-email", "missing-domain@", "name <name@example.com>"} {
		if _, ok := NormalizeEmail(email); ok {
			t.Errorf("NormalizeEmail(%q) accepted invalid input", email)
		}
	}
}

func TestValidPassword(t *testing.T) {
	if !ValidPassword("12345678") {
		t.Fatal("8-character password was rejected")
	}
	if ValidPassword("short") {
		t.Fatal("short password was accepted")
	}
}
