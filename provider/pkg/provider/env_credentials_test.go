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
	"testing"

	"github.com/blang/semver"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

// TestCredentialsFor_SourceOrder pins resource > provider config > environment
// > profile file (#1951).
func TestCredentialsFor_SourceOrder(t *testing.T) {
	useProfileFile(t, twoProfiles)
	t.Setenv("MODAL_TOKEN_ID", "ak-env")
	t.Setenv("MODAL_TOKEN_SECRET", "as-env")
	t.Setenv("MODAL_SERVER_ADDR", "env.example:443")

	id, secret, addr, err := credentialsFor(ProviderConfig{}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"ak-env", "as-env", "env.example:443"}, []string{id, secret, addr})

	cfg := ProviderConfig{TokenID: strPtr("ak-cfg"), TokenSecret: strPtr("as-cfg"), ServerAddr: strPtr("cfg.example:443")}
	id, secret, addr, err = credentialsFor(cfg, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"ak-cfg", "as-cfg", "cfg.example:443"}, []string{id, secret, addr})

	id, secret, _, err = credentialsFor(cfg, strPtr("ak-res"), strPtr("as-res"))
	require.NoError(t, err)
	require.Equal(t, []string{"ak-res", "as-res"}, []string{id, secret})

	// Nothing in the environment: the profile file still applies.
	for _, v := range []string{"MODAL_TOKEN_ID", "MODAL_TOKEN_SECRET", "MODAL_SERVER_ADDR"} {
		t.Setenv(v, "")
	}
	id, secret, _, err = credentialsFor(ProviderConfig{}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"ak-work", "as-work"}, []string{id, secret})

	// Nothing anywhere.
	useProfileFile(t, "")
	_, _, _, err = credentialsFor(ProviderConfig{}, nil, nil)
	require.ErrorContains(t, err, "no Modal credentials found")
}

// The environment credentials count as provider-level credentials, so the
// active profile's environment is not borrowed (#1951).
func TestResolveEnv_EnvCredentialsDoNotBorrowProfileEnvironment(t *testing.T) {
	useProfileFile(t, twoProfiles)
	t.Setenv("MODAL_TOKEN_ID", "ak-env")
	t.Setenv("MODAL_TOKEN_SECRET", "as-env")
	require.Equal(t, "", resolveEnv(nil, ProviderConfig{}))
}

// TestImportedResources_EnvOnlyCredentials reproduces #1951: after `pulumi
// import` the provider is configured without the SetDefault environment
// defaults, yet Read, Update and Delete must reach the server with the
// credentials and endpoint from the environment.
func TestImportedResources_EnvOnlyCredentials(t *testing.T) {
	fake, addr := startFakeModal(t)
	creator := serverFor(t, addr, "")
	urn := modalUrn("Volume", "vol")
	created, err := creator.Create(pgp.CreateRequest{Urn: urn, Properties: pm(map[string]property.Value{"name": str("data")})})
	require.NoError(t, err)

	useProfileFile(t, "")
	t.Setenv("MODAL_TOKEN_ID", "ak-test")
	t.Setenv("MODAL_TOKEN_SECRET", "as-test")
	t.Setenv("MODAL_SERVER_ADDR", addr)
	p, err := New()
	require.NoError(t, err)
	s, err := integration.NewServer(t.Context(), "modal", semver.MustParse("0.0.1"), integration.WithProvider(p))
	require.NoError(t, err)
	require.NoError(t, s.Configure(pgp.ConfigureRequest{Args: property.NewMap(map[string]property.Value{})}))

	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties})
	require.NoError(t, err)
	require.Equal(t, created.ID, read.ID)

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: created.Properties}))
	fake.mu.Lock()
	_, still := fake.volumes[created.ID]
	fake.mu.Unlock()
	require.False(t, still, "volume should be deleted through env-only credentials")
}
