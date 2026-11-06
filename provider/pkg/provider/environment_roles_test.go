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

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

// #1535 (part 2): the default member role is an in-place Environment input,
// read back on refresh, importable and left alone while unset.
func TestEnvironment_DefaultMemberRole(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Environment", "e")

	inputs := pm(map[string]property.Value{"name": str("staging"), "defaultMemberRole": str("viewer")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	require.Equal(t, "viewer", created.Properties.Get("defaultMemberRole").AsString())
	require.Equal(t, pb.EnvironmentRole_ENVIRONMENT_ROLE_VIEWER, *f.envs[created.ID].defaultRole)

	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Drift made outside Pulumi shows up as an update.
	r := pb.EnvironmentRole_ENVIRONMENT_ROLE_NO_ACCESS
	f.envs[created.ID].defaultRole = &r
	read, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["defaultMemberRole"].Kind)

	// Update in place.
	next := pm(map[string]property.Value{"name": str("staging"), "defaultMemberRole": str("contributor")})
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "contributor", upd.Properties.Get("defaultMemberRole").AsString())
	require.Equal(t, pb.EnvironmentRole_ENVIRONMENT_ROLE_CONTRIBUTOR, *f.envs[created.ID].defaultRole)

	// Unset leaves it as is.
	unset := pm(map[string]property.Value{"name": str("staging")})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: upd.Properties, Inputs: unset, OldInputs: next})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Import from the ID alone adopts it.
	imp, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: property.NewMap(nil), Inputs: property.NewMap(nil)})
	require.NoError(t, err)
	require.Equal(t, "contributor", imp.Inputs.Get("defaultMemberRole").AsString())

	// Deleted outside Pulumi: Read reports it gone.
	delete(f.envs, created.ID)
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: upd.Properties, Inputs: next})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

func TestEnvironment_DefaultMemberRoleValidation(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Environment", "e")
	chk, err := s.Check(pgp.CheckRequest{Urn: urn, Inputs: pm(map[string]property.Value{"name": str("x"), "defaultMemberRole": str("admin")})})
	require.NoError(t, err)
	require.NotEmpty(t, chk.Failures)
	chk, err = s.Check(pgp.CheckRequest{Urn: urn, Inputs: pm(map[string]property.Value{"name": str("x"), "defaultMemberRole": str("no_access")})})
	require.NoError(t, err)
	require.Empty(t, chk.Failures)
	chk, err = s.Check(pgp.CheckRequest{Urn: urn, Inputs: pm(map[string]property.Value{"name": str("x"), "defaultMemberRole": property.New(property.Computed)})})
	require.NoError(t, err)
	require.Empty(t, chk.Failures)
}
