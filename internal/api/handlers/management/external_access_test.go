package management

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExternalAccessManagementPatchAndRotation(t *testing.T) {
	setupAPIKeyEntriesTestDB(t)
	if err := usage.UpsertAPIKey(usage.APIKeyRow{Key: "management-fixture", Name: "Laptop", DailyLimit: 300, AllowedModels: []string{"gemini-3.8-flash-tiered"}}); err != nil {
		t.Fatal(err)
	}
	before := usage.GetAPIKey("management-fixture")
	h := NewHandler(&config.Config{}, "", nil)
	for _, body := range []string{`{"index":0,"value":{"external-access":true}}`, `{"index":0,"value":{"key":"rotated-management-fixture"}}`} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPatch, "/api-key-entries", bytes.NewBufferString(body))
		h.PatchAPIKeyEntry(c)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	after := usage.GetAPIKey("rotated-management-fixture")
	if after == nil || !after.ExternalAccess || after.ID != before.ID || after.DailyLimit != 300 || len(after.AllowedModels) != 1 {
		t.Fatal("management write lost key policy")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api-key-entries", nil)
	h.GetAPIKeyEntries(c)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"external-access":true`)) {
		t.Fatal("management GET lost external flag")
	}
}
