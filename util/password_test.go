package util

import "testing"

func TestPasswordDetection(t *testing.T) {
	plain := "$2plaintext"
	if IsHashedPassword(plain) {
		t.Fatalf("plaintext %q was detected as bcrypt", plain)
	}
	if !CheckPassword(plain, plain) {
		t.Fatal("plaintext beginning with $2 cannot be checked")
	}

	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !IsHashedPassword(hash) || !CheckPassword(plain, hash) {
		t.Fatal("generated bcrypt hash was not detected or checked")
	}
}
