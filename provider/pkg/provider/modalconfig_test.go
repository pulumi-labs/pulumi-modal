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
	"os"
	"path/filepath"
	"testing"

	"github.com/blang/semver"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

// TestMain keeps every test off the developer's real ~/.modal.toml: the
// profile file is the provider's last credential source, so a machine that has
// run `modal token new` would otherwise leak credentials into tests that
// expect none. Tests that exercise the file set MODAL_CONFIG_PATH themselves.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "modal-config-test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("MODAL_CONFIG_PATH", filepath.Join(dir, "absent.toml"))
	_ = os.Unsetenv("MODAL_PROFILE")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// useProfileFile writes content to a temp .modal.toml and points the provider
// at it, with no credential environment variables set.
func useProfileFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".modal.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	t.Setenv("MODAL_CONFIG_PATH", path)
	t.Setenv("MODAL_PROFILE", "")
	for _, v := range []string{"MODAL_TOKEN_ID", "MODAL_TOKEN_SECRET", "MODAL_ENVIRONMENT", "MODAL_SERVER_ADDR"} {
		t.Setenv(v, "")
	}
	return path
}

const twoProfiles = `
[personal]
token_id = "ak-personal"
token_secret = "as-personal"
environment = "dev"

[work]
token_id = "ak-work"
token_secret = "as-work"
environment = "prod"
server_url = "https://api.modal.com:443"
active = true
`

func TestLoadModalProfile_ActiveProfile(t *testing.T) {
	path := useProfileFile(t, twoProfiles)
	p, err := loadModalProfile()
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Equal(t, "work", p.Name)
	require.Equal(t, path, p.Path)
	require.Equal(t, "ak-work", p.TokenID)
	require.Equal(t, "as-work", p.TokenSecret)
	require.Equal(t, "prod", p.Environment)
	require.Equal(t, "api.modal.com:443", p.serverAddr())
}

func TestLoadModalProfile_ModalProfileWinsOverActive(t *testing.T) {
	useProfileFile(t, twoProfiles)
	t.Setenv("MODAL_PROFILE", "personal")
	p, err := loadModalProfile()
	require.NoError(t, err)
	require.Equal(t, "ak-personal", p.TokenID)
	require.Equal(t, "dev", p.Environment)
	require.Empty(t, p.serverAddr())
}

func TestLoadModalProfile_DefaultProfileWhenNothingActive(t *testing.T) {
	useProfileFile(t, "[other]\ntoken_id = \"ak-o\"\ntoken_secret = \"as-o\"\n[default]\ntoken_id = \"ak-d\"\ntoken_secret = \"as-d\"\n")
	p, err := loadModalProfile()
	require.NoError(t, err)
	require.Equal(t, "ak-d", p.TokenID)
}

func TestLoadModalProfile_NothingSelected(t *testing.T) {
	useProfileFile(t, "[a]\ntoken_id = \"ak-a\"\ntoken_secret = \"as-a\"\n[b]\ntoken_id = \"ak-b\"\ntoken_secret = \"as-b\"\n")
	p, err := loadModalProfile()
	require.NoError(t, err)
	require.Nil(t, p, "no MODAL_PROFILE, no active profile and no default: nothing to use")
}

func TestLoadModalProfile_MissingFileIsNotAnError(t *testing.T) {
	useProfileFile(t, "")
	t.Setenv("MODAL_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.toml"))
	p, err := loadModalProfile()
	require.NoError(t, err)
	require.Nil(t, p)
}

func TestLoadModalProfile_Errors(t *testing.T) {
	t.Run("malformed file", func(t *testing.T) {
		useProfileFile(t, "[work\ntoken_id = ")
		_, err := loadModalProfile()
		require.ErrorContains(t, err, "parsing")
	})
	t.Run("unknown MODAL_PROFILE", func(t *testing.T) {
		useProfileFile(t, twoProfiles)
		t.Setenv("MODAL_PROFILE", "nonesuch")
		_, err := loadModalProfile()
		require.ErrorContains(t, err, `MODAL_PROFILE="nonesuch"`)
	})
	t.Run("MODAL_PROFILE with no file", func(t *testing.T) {
		useProfileFile(t, "")
		t.Setenv("MODAL_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.toml"))
		t.Setenv("MODAL_PROFILE", "work")
		_, err := loadModalProfile()
		require.ErrorContains(t, err, "does not exist")
	})
}

func TestModalProfileServerAddr(t *testing.T) {
	for in, want := range map[string]string{
		"":                          "",
		"https://api.modal.com:443": "api.modal.com:443",
		"https://api.modal.com":     "api.modal.com:443",
		"http://127.0.0.1:8080":     "127.0.0.1:8080",
		"api.modal.com:443":         "api.modal.com:443",
	} {
		require.Equal(t, want, (&modalProfile{ServerURL: in}).serverAddr(), in)
	}
}

func TestClientFor_CredentialPrecedence(t *testing.T) {
	useProfileFile(t, twoProfiles)

	// Provider config (which already folds in MODAL_TOKEN_*) beats the file:
	// the client is built, and the profile's server_url is not borrowed.
	c, err := clientFor(ProviderConfig{TokenID: strPtr("ak-cfg"), TokenSecret: strPtr("as-cfg")}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	// Nothing configured: the active profile supplies the credentials.
	c, err = clientFor(ProviderConfig{}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	// Only one half configured: the file does not fill the gap.
	_, err = clientFor(ProviderConfig{TokenID: strPtr("ak-cfg")}, nil, nil)
	require.ErrorContains(t, err, "both tokenId and tokenSecret are required")
}

func TestClientFor_MissingCredentialsMessage(t *testing.T) {
	useProfileFile(t, "")
	t.Setenv("MODAL_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.toml"))
	_, err := clientFor(ProviderConfig{}, nil, nil)
	require.Error(t, err)
	msg := err.Error()
	require.NotContains(t, msg, "modal: modal:")
	for _, source := range []string{"modal:tokenId", "MODAL_TOKEN_ID", "~/.modal.toml", "MODAL_PROFILE", "modal token new"} {
		require.Contains(t, msg, source)
	}

	// A profile without a token (for example an OAuth-only one) says so.
	useProfileFile(t, "[work]\nactive = true\nenvironment = \"prod\"\n")
	_, err = clientFor(ProviderConfig{}, nil, nil)
	require.ErrorContains(t, err, `profile "work"`)

	// A broken file is reported, not mistaken for absent credentials.
	useProfileFile(t, "[work\n")
	_, err = clientFor(ProviderConfig{}, nil, nil)
	require.ErrorContains(t, err, "parsing")
}

func TestResolveEnv_Precedence(t *testing.T) {
	useProfileFile(t, twoProfiles)

	// Profile environment applies only when the profile supplies the credentials.
	require.Equal(t, "prod", resolveEnv(nil, ProviderConfig{}))
	require.Equal(t, "", resolveEnv(nil, ProviderConfig{TokenID: strPtr("ak"), TokenSecret: strPtr("as")}))
	// modal:environment / MODAL_ENVIRONMENT and the resource input beat the profile.
	require.Equal(t, "cfg", resolveEnv(nil, ProviderConfig{Environment: strPtr("cfg")}))
	require.Equal(t, "res", resolveEnv(strPtr("res"), ProviderConfig{Environment: strPtr("cfg")}))
	// A broken or missing file contributes nothing here.
	t.Setenv("MODAL_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.toml"))
	require.Equal(t, "", resolveEnv(nil, ProviderConfig{}))
}

// A default provider with no credentials in config or environment still
// works against a profile file, end to end: token, endpoint and environment
// all come from the active profile.
func TestProfileFile_EndToEnd(t *testing.T) {
	fake, addr := startFakeModal(t)
	useProfileFile(t, "[default]\nactive = true\ntoken_id = \"ak-test\"\ntoken_secret = \"as-test\"\nenvironment = \"staging\"\nserver_url = \"http://"+addr+"\"\n")

	p, err := New()
	require.NoError(t, err)
	s, err := integration.NewServer(t.Context(), "modal", semver.MustParse("0.0.1"), integration.WithProvider(p))
	require.NoError(t, err)
	require.NoError(t, s.Configure(pgp.ConfigureRequest{Args: pm(map[string]property.Value{})}))

	urn := modalUrn("Queue", "q")
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: pm(map[string]property.Value{"name": str("jobs")})})
	require.NoError(t, err)
	require.Equal(t, "staging", getStr(t, created.Properties, "environment"))
	fake.mu.Lock()
	require.Len(t, fake.queues, 1)
	fake.mu.Unlock()
}

// A resource that brings its own token pair uses neither the profile's
// credentials nor its environment: the profile applies only when it supplies
// the credentials.
func TestProfileFile_ResourceTokensDoNotBorrowProfileEnvironment(t *testing.T) {
	fake, addr := startFakeModal(t)
	useProfileFile(t, "[default]\nactive = true\ntoken_id = \"ak-other\"\ntoken_secret = \"as-other\"\nenvironment = \"staging\"\n")

	p, err := New()
	require.NoError(t, err)
	s, err := integration.NewServer(t.Context(), "modal", semver.MustParse("0.0.1"), integration.WithProvider(p))
	require.NoError(t, err)
	require.NoError(t, s.Configure(pgp.ConfigureRequest{Args: pm(map[string]property.Value{"serverAddr": str(addr)})}))

	urn := modalUrn("Queue", "q")
	in := pm(map[string]property.Value{"name": str("jobs"), "tokenId": str("ak-test"), "tokenSecret": str("as-test")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.NoError(t, err)
	require.Equal(t, "main", getStr(t, created.Properties, "environment"), "the account default, not the profile's staging")

	// And a Diff with the same resource tokens does not see an environment change.
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: in, OldInputs: in})
	require.NoError(t, err)
	requireNoChanges(t, d)
	fake.mu.Lock()
	require.Len(t, fake.queues, 1)
	fake.mu.Unlock()
}
