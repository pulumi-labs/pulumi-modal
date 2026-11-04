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

	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

// The live Modal API rejects CREATE_FAIL_IF_EXISTS for Apps with "Invalid
// object creation type" and answers a lookup of a missing App with an empty ID
// (the fake mirrors both), so App create must pre-check the name itself.
func TestApp_CreateDuplicateAndFreedName(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("App", "a")

	inputs := pm(map[string]property.Value{"name": str("my-app")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "main", getStr(t, created.Properties, "environment"))

	// A second App with the same name still fails, with a clear message.
	_, err = s.Create(pgp.CreateRequest{Urn: modalUrn("App", "b"), Properties: inputs})
	require.ErrorContains(t, err, "already exists")

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Tags are applied on create.
	tagged := pm(map[string]property.Value{
		"name": str("tagged-app"),
		"tags": property.New(map[string]property.Value{"team": property.New("infra")}),
	})
	tc, err := s.Create(pgp.CreateRequest{Urn: modalUrn("App", "t"), Properties: tagged})
	require.NoError(t, err)
	read, err := s.Read(pgp.ReadRequest{ID: tc.ID, Urn: modalUrn("App", "t")})
	require.NoError(t, err)
	require.Equal(t, "tagged-app", getStr(t, read.Properties, "name"))

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: created.Properties}))
	// After delete the name is free again.
	_, err = s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
}

// The live API answers a lookup of a missing App with success and an empty ID
// (the fake mirrors it); getApp must report the App as not found rather than
// asking for the tags of App "".
func TestGetApp_MissingAppIsNotFound(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	_, err := invoke(t, s, "getApp", map[string]property.Value{"name": str("ghost")})
	require.ErrorContains(t, err, `app "ghost" was not found`)
	require.NotContains(t, err.Error(), "not a valid App ID")

	created, err := s.Create(pgp.CreateRequest{Urn: modalUrn("App", "a"), Properties: pm(map[string]property.Value{"name": str("real")})})
	require.NoError(t, err)
	out, err := invoke(t, s, "getApp", map[string]property.Value{"name": str("real")})
	require.NoError(t, err)
	require.Equal(t, created.ID, getStr(t, out, "appId"))
}
