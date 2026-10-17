package browser

import (
	"ant-chrome/backend/internal/database"
	"path/filepath"
	"testing"
)

func TestSQLiteProfileDAOPersistsMemoryLimitMB(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "profiles.db"))
	if err != nil {
		t.Fatalf("NewDB returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	dao := NewSQLiteProfileDAO(db.GetConn())
	profile := &Profile{
		ProfileId:       "profile-memory-limit",
		ProfileName:     "memory limit",
		MemoryLimitMB:   768,
		FingerprintArgs: []string{},
		LaunchArgs:      []string{},
		Tags:            []string{},
		Keywords:        []string{},
		CreatedAt:       "2026-07-26T00:00:00Z",
		UpdatedAt:       "2026-07-26T00:00:00Z",
	}

	if err := dao.Upsert(profile); err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	stored, err := dao.GetById(profile.ProfileId)
	if err != nil {
		t.Fatalf("GetById returned error: %v", err)
	}
	if stored.MemoryLimitMB != profile.MemoryLimitMB {
		t.Fatalf("MemoryLimitMB = %d, want %d", stored.MemoryLimitMB, profile.MemoryLimitMB)
	}

	listed, err := dao.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List length = %d, want 1", len(listed))
	}
	if listed[0].MemoryLimitMB != profile.MemoryLimitMB {
		t.Fatalf("listed MemoryLimitMB = %d, want %d", listed[0].MemoryLimitMB, profile.MemoryLimitMB)
	}
}

func TestSQLiteProfileDAOPersistsClearCacheOnClose(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "profiles_cache.db"))
	if err != nil {
		t.Fatalf("NewDB returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	dao := NewSQLiteProfileDAO(db.GetConn())
	profile := &Profile{
		ProfileId:         "profile-clear-cache",
		ProfileName:       "clear cache on close",
		ClearCacheOnClose: ClearCacheOnCloseEnabled,
		FingerprintArgs:   []string{},
		LaunchArgs:        []string{},
		Tags:              []string{},
		Keywords:          []string{},
		CreatedAt:         "2026-10-03T00:00:00Z",
		UpdatedAt:         "2026-10-03T00:00:00Z",
	}

	if err := dao.Upsert(profile); err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	stored, err := dao.GetById(profile.ProfileId)
	if err != nil {
		t.Fatalf("GetById returned error: %v", err)
	}
	if stored.ClearCacheOnClose != ClearCacheOnCloseEnabled {
		t.Fatalf("ClearCacheOnClose = %q, want %q", stored.ClearCacheOnClose, ClearCacheOnCloseEnabled)
	}

	listed, err := dao.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List length = %d, want 1", len(listed))
	}
	if listed[0].ClearCacheOnClose != ClearCacheOnCloseEnabled {
		t.Fatalf("listed ClearCacheOnClose = %q, want %q", listed[0].ClearCacheOnClose, ClearCacheOnCloseEnabled)
	}
}
