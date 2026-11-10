package claude_code

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"time"
)

// The cache contains the original remote observation, never the time it was
// rendered. Legacy entries lack identity and reset information and are ignored.
type usageCacheEntry struct {
	AccountID string    `json:"account_id"`
	Identity  string    `json:"identity"`
	Pct       float64   `json:"pct"`
	TS        time.Time `json:"ts"`
	Reset     time.Time `json:"reset"`
}

func UsageCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "openusage", "statusline-5h.json")
}

func currentCacheIdentity() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return ""
	}
	var account accountConfig
	if json.Unmarshal(data, &account) != nil || account.OAuthAccount == nil || account.OAuthAccount.AccountUUID == "" || account.OAuthAccount.OrganizationUUID == "" {
		return ""
	}
	return cacheIdentity(account.OAuthAccount.AccountUUID, account.OAuthAccount.OrganizationUUID)
}

func cacheIdentity(account, org string) string {
	sum := sha256.Sum256([]byte(account + "\x00" + org))
	return hex.EncodeToString(sum[:])
}

// WriteFiveHourCache stores a fresh, account-scoped observation atomically.
func WriteFiveHourCache(accountID string, pct float64, observedAt, resetAt time.Time) {
	identity := currentCacheIdentity()
	now := time.Now()
	if accountID == "" || identity == "" || math.IsNaN(pct) || math.IsInf(pct, 0) || pct < 0 || pct > 100 || observedAt.IsZero() || observedAt.After(now) || !resetAt.After(now) {
		return
	}
	p := UsageCachePath()
	if p == "" || os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	b, err := json.Marshal(usageCacheEntry{AccountID: accountID, Identity: identity, Pct: pct, TS: observedAt, Reset: resetAt})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".statusline-5h-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return
	}
	if tmp.Close() != nil {
		return
	}
	_ = os.Rename(name, p)
}

// ReadFiveHourCache rejects unknown identities, legacy entries and expired resets.
func ReadFiveHourCache(accountID string) (pct float64, observedAt, resetAt time.Time, ok bool) {
	identity := currentCacheIdentity()
	if accountID == "" || identity == "" {
		return 0, time.Time{}, time.Time{}, false
	}
	b, err := os.ReadFile(UsageCachePath())
	if err != nil {
		return 0, time.Time{}, time.Time{}, false
	}
	var c usageCacheEntry
	now := time.Now()
	if json.Unmarshal(b, &c) != nil || c.AccountID != accountID || c.Identity != identity || c.TS.IsZero() || c.TS.After(now) || !c.Reset.After(now) || math.IsNaN(c.Pct) || math.IsInf(c.Pct, 0) || c.Pct < 0 || c.Pct > 100 {
		return 0, time.Time{}, time.Time{}, false
	}
	return c.Pct, c.TS, c.Reset, true
}

func invalidateFiveHourCache(accountID string) {
	if accountID == "" {
		return
	}
	p := UsageCachePath()
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var c usageCacheEntry
	if json.Unmarshal(b, &c) == nil && c.AccountID == accountID {
		_ = os.Remove(p)
	}
}
