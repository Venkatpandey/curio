package config

import "testing"

func TestConfigValidation(t *testing.T) {
	for _, key := range []string{"CURIO_PORT", "CURIO_DATA_DIR", "CURIO_BASE_URL", "CURIO_ACCESS_PASSWORD", "CURIO_GUEST_ENABLED", "CURIO_WIKIMEDIA_ENABLED", "CURIO_FEEDS_ENABLED", "CURIO_LOG_LEVEL"} {
		t.Setenv(key, "")
	}
	if _, err := Load(); err == nil {
		t.Fatal("missing shared password accepted")
	}
	t.Setenv("CURIO_ACCESS_PASSWORD", "household-test-password")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "8080" || c.SecureCookies() {
		t.Fatal("incorrect defaults")
	}
	if !c.FeedsEnabled {
		t.Fatal("feed default disabled")
	}
	t.Setenv("CURIO_FEEDS_ENABLED", "false")
	c, err = Load()
	if err != nil || c.FeedsEnabled {
		t.Fatal("cannot disable feeds", err)
	}
	for _, bad := range []string{"https://example.com/path", "ftp://example.com", "https://user:password@example.com", "https://example.com?key=secret"} {
		t.Setenv("CURIO_BASE_URL", bad)
		if _, err = Load(); err == nil {
			t.Fatalf("accepted invalid origin %s", bad)
		}
	}
	t.Setenv("CURIO_BASE_URL", "https://curio.example.com")
	c, err = Load()
	if err != nil || !c.SecureCookies() {
		t.Fatalf("HTTPS configuration: %v", err)
	}
	for _, pair := range [][2]string{{"CURIO_PORT", "0"}, {"CURIO_PORT", "65536"}, {"CURIO_GUEST_ENABLED", "perhaps"}, {"CURIO_FEEDS_ENABLED", "perhaps"}, {"CURIO_LOG_LEVEL", "secret"}} {
		t.Run(pair[0]+pair[1], func(t *testing.T) {
			t.Setenv(pair[0], pair[1])
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
