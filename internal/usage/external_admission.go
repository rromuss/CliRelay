package usage

import (
	"context"
	sqlapikey "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/sqlite/apikey"
	"time"
)

// AdmitExternalCall is the narrow storage bridge; it exposes no DB or credential.
func AdmitExternalCall(ctx context.Context, id string, daily, rpm int, now time.Time) error {
	return sqlapikey.NewStore(getDB()).AdmitExternal(ctx, id, daily, rpm, now)
}
