package claude_code

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setCacheIdentity(t *testing.T, account, org string) {
	t.Helper()
	data := `{"oauthAccount":{"accountUuid":"` + account + `","organizationUuid":"` + org + `"}}`
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".claude.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFiveHourCacheIdentityAndReset(t *testing.T) {
	setTempHome(t)
	setCacheIdentity(t, "user-a", "org-a")
	observed := time.Now().Add(-time.Minute).UTC()
	reset := observed.Add(time.Hour)
	WriteFiveHourCache("account-a", 42.5, observed, reset)
	pct, gotObserved, gotReset, ok := ReadFiveHourCache("account-a")
	if !ok || pct != 42.5 || !gotObserved.Equal(observed) || !gotReset.Equal(reset) {
		t.Fatalf("unexpected cache: %v %v %v %v", pct, gotObserved, gotReset, ok)
	}
	if _, _, _, ok := ReadFiveHourCache("account-b"); ok {
		t.Fatal("another account must not read cache")
	}
	setCacheIdentity(t, "user-b", "org-a")
	if _, _, _, ok := ReadFiveHourCache("account-a"); ok {
		t.Fatal("a changed identity must not read cache")
	}
	setCacheIdentity(t, "user-a", "org-a")
	WriteFiveHourCache("account-a", 10, observed, time.Now().Add(-time.Second))
	if err := os.WriteFile(UsageCachePath(), []byte(`{"pct":2,"ts":"2020-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := ReadFiveHourCache("account-a"); ok {
		t.Fatal("legacy unscoped cache must not be read")
	}
}

func TestFiveHourCacheRejectsCorruptionAndExpiry(t *testing.T) {
	setTempHome(t)
	setCacheIdentity(t, "user", "org")
	p := UsageCachePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := ReadFiveHourCache("account"); ok {
		t.Fatal("corrupt cache accepted")
	}
	WriteFiveHourCache("account", 42, time.Now().Add(-time.Minute), time.Now().Add(-time.Second))
	if _, _, _, ok := ReadFiveHourCache("account"); ok {
		t.Fatal("expired reset accepted")
	}
}
