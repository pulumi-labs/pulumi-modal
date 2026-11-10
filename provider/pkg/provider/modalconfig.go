// Copyright 2026, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package provider

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// The Modal CLI (`modal token new`, `modal setup`) writes its credentials to
// ~/.modal.toml, one table per profile:
//
//	[default]
//	token_id = "ak-..."
//	token_secret = "as-..."
//	environment = "main"   # optional
//	active = true
//
// The provider reads the same file as its last credential source, so a user
// who has authenticated the CLI needs no environment variables. The lookup
// follows the Modal clients: MODAL_CONFIG_PATH overrides the file location,
// MODAL_PROFILE names the profile, otherwise the profile marked `active = true`
// (or one called "default") is used.

// modalProfile is the part of a .modal.toml profile the provider uses.
type modalProfile struct {
	Name        string
	Path        string
	TokenID     string
	TokenSecret string
	Environment string
	ServerURL   string
}

type rawModalProfile struct {
	ServerURL   string `toml:"server_url"`
	TokenID     string `toml:"token_id"`
	TokenSecret string `toml:"token_secret"`
	Environment string `toml:"environment"`
	Active      bool   `toml:"active"`
}

// modalConfigPath returns the config file location: MODAL_CONFIG_PATH, else
// ~/.modal.toml.
func modalConfigPath() (string, error) {
	if p := os.Getenv("MODAL_CONFIG_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory to read .modal.toml: %w", err)
	}
	return filepath.Join(home, ".modal.toml"), nil
}

// loadModalProfile reads the config file and selects a profile. It returns
// (nil, nil) when there is nothing to use: no file, or no profile selected and
// none active. It returns an error when the file cannot be read or parsed, or
// when MODAL_PROFILE names a profile that does not exist.
func loadModalProfile() (*modalProfile, error) {
	path, err := modalConfigPath()
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if name := os.Getenv("MODAL_PROFILE"); name != "" {
			return nil, fmt.Errorf("MODAL_PROFILE=%q is set but %s does not exist", name, path)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var file map[string]rawModalProfile
	if err := toml.Unmarshal(content, &file); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	name := os.Getenv("MODAL_PROFILE")
	if name != "" {
		if _, ok := file[name]; !ok {
			return nil, fmt.Errorf("MODAL_PROFILE=%q is not a profile in %s", name, path)
		}
	} else {
		names := make([]string, 0, len(file))
		for n := range file {
			names = append(names, n)
		}
		sort.Strings(names) // deterministic when several profiles claim to be active
		for _, n := range names {
			if file[n].Active {
				name = n
				break
			}
		}
		if _, ok := file["default"]; name == "" && ok {
			name = "default"
		}
	}
	if name == "" {
		return nil, nil
	}
	raw := file[name]
	return &modalProfile{
		Name:        name,
		Path:        path,
		TokenID:     raw.TokenID,
		TokenSecret: raw.TokenSecret,
		Environment: raw.Environment,
		ServerURL:   raw.ServerURL,
	}, nil
}

// serverAddr converts the profile's server_url (https://api.modal.com:443) to
// the host:port form the gRPC dialer takes. It returns "" when unset.
func (p *modalProfile) serverAddr() string {
	if p == nil || p.ServerURL == "" {
		return ""
	}
	if !strings.Contains(p.ServerURL, "://") {
		return p.ServerURL
	}
	u, err := url.Parse(p.ServerURL)
	if err != nil || u.Host == "" {
		return p.ServerURL
	}
	if u.Port() == "" {
		if u.Scheme == "http" {
			return u.Host + ":80"
		}
		return u.Host + ":443"
	}
	return u.Host
}

// hasProviderCredentials reports whether the provider-level config (stack
// config or the MODAL_TOKEN_* variables, which the config defaults fold in)
// supplies a token. The profile file is only a fallback for that case: a
// token from a higher source never borrows the environment or endpoint of
// whichever profile happens to be active.
func hasProviderCredentials(cfg ProviderConfig) bool {
	return derefStr(cfg.TokenID) != "" || derefStr(cfg.TokenSecret) != ""
}

// withResourceTokens returns cfg with the resource's own token pair laid over
// the provider-level one, so the environment resolution below sees which
// source supplies the credentials: a resource that brings its own tokens must
// not take the environment of whichever profile is active.
func withResourceTokens(cfg ProviderConfig, tokenID, tokenSecret *string) ProviderConfig {
	if derefStr(tokenID) != "" {
		cfg.TokenID = tokenID
	}
	if derefStr(tokenSecret) != "" {
		cfg.TokenSecret = tokenSecret
	}
	return cfg
}

// profileEnvironment returns the active profile's environment when the
// provider is using the profile file for credentials, else "". Errors are
// ignored here: clientFor reports an unreadable file when it needs the
// credentials.
func profileEnvironment(cfg ProviderConfig) string {
	if hasProviderCredentials(cfg) {
		return ""
	}
	prof, err := loadModalProfile()
	if err != nil || prof == nil {
		return ""
	}
	return prof.Environment
}

// resolveEnv returns the Modal environment for a resource: the resource-level
// value, else modal:environment / MODAL_ENVIRONMENT, else the profile's
// environment, else "" (the account default).
func resolveEnv(resourceLevel *string, cfg ProviderConfig) string {
	if env := resolveStr(resourceLevel, cfg.Environment); env != "" {
		return env
	}
	// The config default for MODAL_ENVIRONMENT is applied by CheckConfig; read the
	// variable again so a provider that was configured without that default
	// pass (#1673) still honours it.
	if env := os.Getenv("MODAL_ENVIRONMENT"); env != "" {
		return env
	}
	return profileEnvironment(cfg)
}

const credentialsHelp = "set tokenId and tokenSecret on the resource, as the modal:tokenId and modal:tokenSecret provider config, " +
	"or as MODAL_TOKEN_ID and MODAL_TOKEN_SECRET; or authenticate the Modal CLI (`modal token new`) so ~/.modal.toml " +
	"(MODAL_CONFIG_PATH) has a profile, chosen by MODAL_PROFILE or marked active = true, with token_id and token_secret"

func missingCredentialsError(tokenID, tokenSecret string, prof *modalProfile) error {
	switch {
	case tokenID != "" || tokenSecret != "":
		return fmt.Errorf("both tokenId and tokenSecret are required, but only one was found: %s", credentialsHelp)
	case prof != nil:
		return fmt.Errorf("profile %q in %s has no token_id and token_secret, and no other credentials were found: %s", prof.Name, prof.Path, credentialsHelp)
	default:
		return fmt.Errorf("no Modal credentials found: %s", credentialsHelp)
	}
}
