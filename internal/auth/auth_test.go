package auth

import "testing"

func TestPasswordVerification(t *testing.T) {
	hash, err := HashPassword("household-password")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "household-password") {
		t.Fatal("valid password rejected")
	}
	if CheckPassword(hash, "different-password") {
		t.Fatal("wrong password accepted")
	}
	second, err := HashPassword("household-password")
	if err != nil {
		t.Fatal(err)
	}
	if hash == second {
		t.Fatal("salts are not unique")
	}
	for _, bad := range []string{"", "plain-text", "pbkdf2-sha256$1$YQ$Yg", hash + "$extra"} {
		if CheckPassword(bad, "household-password") {
			t.Fatalf("accepted malformed hash %q", bad)
		}
	}
}
