package chserver

import (
	"net/http"

	"github.com/proximile/proxiport/server/api"
)

const BuiltInAuthProviderName = "built-in"

// AuthProviderInfo contains the provider name and the uris to be used
// for either regular or device flow based authorization
type AuthProviderInfo struct {
	AuthProvider      string `json:"auth_provider"`
	SettingsURI       string `json:"settings_uri"`
	DeviceSettingsURI string `json:"device_settings_uri"`
	MaxTokenLifetime  int    `json:"max_token_lifetime"`
}

func (al *APIListener) handleGetAuthProvider(w http.ResponseWriter, req *http.Request) {
	builtInAuthProvider := AuthProviderInfo{
		AuthProvider:     BuiltInAuthProviderName,
		SettingsURI:      "",
		MaxTokenLifetime: al.config.API.MaxTokenLifeTimeHours,
	}
	al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(builtInAuthProvider))
}
