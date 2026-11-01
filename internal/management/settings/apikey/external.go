package apikey

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"time"
)

func (s *Service) ExternalEntry(key string) *config.APIKeyEntry {
	row := usage.GetAPIKey(key)
	if row == nil || row.Disabled {
		return nil
	}
	entries := usage.EffectiveAPIKeyRows([]usage.APIKeyRow{*row})
	entry := entries[0].ToConfigEntry()
	return &entry
}

func (s *Service) AdmitExternal(ctx context.Context, entry config.APIKeyEntry, now time.Time) error {
	return usage.AdmitExternalCall(ctx, entry.ID, entry.DailyLimit, entry.RPMLimit, now)
}
