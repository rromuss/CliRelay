package apikey

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrExternalDailyLimit = errors.New("external daily limit exceeded")
var ErrExternalRateLimit = errors.New("external rate limit exceeded")

func initExternalUsage(db *sql.DB) {
	// Additive tables deliberately survive rollback to the previous binary.
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS external_api_usage (
	 api_key_id TEXT NOT NULL, day_utc TEXT NOT NULL, admitted_count INTEGER NOT NULL,
	 PRIMARY KEY(api_key_id, day_utc));
	 CREATE TABLE IF NOT EXISTS external_api_calls (api_key_id TEXT NOT NULL, started_ns INTEGER NOT NULL);
	 CREATE INDEX IF NOT EXISTS external_api_calls_key_time ON external_api_calls(api_key_id, started_ns);`)
}

// AdmitExternal serializes admission, not completed usage. Errors fail closed.
// The first write acquires SQLite's writer lock before inspecting the RPM window.
func (s Store) AdmitExternal(ctx context.Context, id string, daily, rpm int, now time.Time) error {
	if s.db == nil || id == "" || daily <= 0 || rpm <= 0 {
		return errors.New("external admission unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	err = tx.QueryRowContext(ctx, `INSERT INTO external_api_usage(api_key_id,day_utc,admitted_count)
	 VALUES(?,?,1) ON CONFLICT(api_key_id,day_utc) DO UPDATE SET admitted_count=admitted_count+1
	 WHERE admitted_count < ? RETURNING admitted_count`, id, now.UTC().Format("2006-01-02"), daily).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExternalDailyLimit
	}
	if err != nil {
		return err
	}
	cutoff := now.Add(-time.Minute).UnixNano()
	if _, err = tx.ExecContext(ctx, `DELETE FROM external_api_calls WHERE api_key_id=? AND started_ns<=?`, id, cutoff); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM external_api_calls WHERE api_key_id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count >= rpm {
		return ErrExternalRateLimit
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO external_api_calls(api_key_id,started_ns) VALUES(?,?)`, id, now.UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}
