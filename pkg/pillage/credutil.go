package pillage

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
)

// CredentialSnippet returns a short description of the credentials
// for logging purposes. Sensitive values are truncated.
func CredentialSnippet(cfg *authn.AuthConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.RegistryToken != "" {
		token := cfg.RegistryToken
		if len(token) > 6 {
			token = token[:6] + "..."
		}
		return fmt.Sprintf("token %s", token)
	}
	if cfg.IdentityToken != "" {
		token := cfg.IdentityToken
		if len(token) > 6 {
			token = token[:6] + "..."
		}
		return fmt.Sprintf("idtoken %s", token)
	}
	if cfg.Password != "" || cfg.Username != "" {
		pwd := cfg.Password
		if len(pwd) > 6 {
			pwd = pwd[:6] + "..."
		}
		return fmt.Sprintf("user %s pass %s", cfg.Username, pwd)
	}
	return "anonymous"
}
