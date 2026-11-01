package apikey

import (
	"context"
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func externalStore(t *testing.T) (Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	InitTable(db)
	return NewStore(db), path
}

func TestExternalMigrationPreservesLegacyRows(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	legacy := strings.Replace(createAPIKeysTableSQL, ",\n  external_access   INTEGER NOT NULL DEFAULT 0", "", 1)
	if legacy == createAPIKeysTableSQL {
		t.Fatal("legacy fixture no longer matches schema")
	}
	if _, err = db.Exec(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO api_keys(key,id,name,daily_limit,allowed_models) VALUES('legacy-fixture','legacy-id','Hermes',41,'["private-model"]')`); err != nil {
		t.Fatal(err)
	}
	InitTable(db)
	r := NewStore(db).Get("legacy-fixture")
	if r == nil || r.ID != "legacy-id" || r.ExternalAccess || r.Name != "Hermes" || r.DailyLimit != 41 || len(r.AllowedModels) != 1 || r.AllowedModels[0] != "private-model" {
		t.Fatalf("migration changed private policy: %+v", r)
	}
}
func TestExternalDefaultsRotationAndProfileIsolation(t *testing.T) {
	s, _ := externalStore(t)
	if err := s.Upsert(APIKeyRow{Key: "internal-fixture", Name: "Hermes", DailyLimit: 9}); err != nil {
		t.Fatal(err)
	}
	old := s.Get("internal-fixture")
	if old == nil || old.ExternalAccess || old.DailyLimit != 9 {
		t.Fatalf("private migration: %+v", old)
	}
	old.ExternalAccess = true
	if err := s.Upsert(*old); err != nil {
		t.Fatal(err)
	}
	old = s.Get(old.Key)
	if !old.ExternalAccess {
		t.Fatal("flag not persisted")
	}
	id := old.ID
	old.Key = "rotated-fixture"
	if err := s.UpdateByID(*old); err != nil {
		t.Fatal(err)
	}
	got := s.Get(old.Key)
	if got == nil || got.ID != id || !got.ExternalAccess || s.Get("internal-fixture") != nil {
		t.Fatal("rotation changed identity or external access")
	}
	got.PermissionProfileID = "profile"
	got = ptrRow(EffectiveAPIKeyRowWithProfiles(*got, []PermissionProfileSnapshot{{ID: "profile", DailyLimit: 3}}))
	if !got.ExternalAccess {
		t.Fatal("profile overwrote key-owned flag")
	}
	if err := s.ReplaceAll([]APIKeyRow{*got}); err != nil {
		t.Fatal(err)
	}
	if !s.Get(old.Key).ExternalAccess {
		t.Fatal("bulk replacement lost flag")
	}
}
func ptrRow(r APIKeyRow) *APIKeyRow { return &r }

func TestExternalAdmissionAtomicPersistentAndRolling(t *testing.T) {
	s, path := externalStore(t)
	now := time.Date(2026, 11, 1, 23, 59, 50, 0, time.UTC)
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.AdmitExternal(context.Background(), "stable-key", 3, 20, now)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrExternalDailyLimit) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("admitted %d", accepted.Load())
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	other := NewStore(db)
	if err := other.AdmitExternal(context.Background(), "stable-key", 3, 20, now); !errors.Is(err, ErrExternalDailyLimit) {
		t.Fatalf("counter not durable: %v", err)
	}
	if err := other.AdmitExternal(context.Background(), "stable-key", 3, 20, now.Add(20*time.Second)); err != nil {
		t.Fatal("UTC day did not reset", err)
	}
	if err := s.AdmitExternal(context.Background(), "rpm", 300, 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitExternal(context.Background(), "rpm", 300, 1, now.Add(20*time.Second)); !errors.Is(err, ErrExternalRateLimit) {
		t.Fatal("minute boundary bypass", err)
	}
	if err := s.AdmitExternal(context.Background(), "rpm", 300, 1, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(nil).AdmitExternal(context.Background(), "id", 300, 10, now); err == nil {
		t.Fatal("unavailable DB admitted request")
	}
}
