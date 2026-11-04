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

// The live EnvironmentUpdate response is sparse. Update must keep the
// environment's ID and name in state, so a later Diff reports no changes and a
// second Update still finds the environment.
func TestEnvironment_UpdateKeepsIdentity(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Environment", "e")

	inputs := pm(map[string]property.Value{"name": str("staging"), "maxConcurrentTasks": num(10)})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)

	next := pm(map[string]property.Value{"name": str("staging"), "maxConcurrentTasks": num(20)})
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "staging", getStr(t, upd.Properties, "name"))
	require.Equal(t, created.ID, getStr(t, upd.Properties, "environmentId"))
	require.Equal(t, float64(20), upd.Properties.Get("maxConcurrentTasks").AsNumber())

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: upd.Properties, Inputs: next, OldInputs: next})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// A rename, then another update against the renamed state.
	renamed := pm(map[string]property.Value{"name": str("staging2"), "maxConcurrentTasks": num(20)})
	upd2, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: upd.Properties, Inputs: renamed, OldInputs: next})
	require.NoError(t, err)
	require.Equal(t, "staging2", getStr(t, upd2.Properties, "name"))
	third := pm(map[string]property.Value{"name": str("staging2"), "maxConcurrentTasks": num(30)})
	upd3, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: upd2.Properties, Inputs: third, OldInputs: renamed})
	require.NoError(t, err)
	require.Equal(t, float64(30), upd3.Properties.Get("maxConcurrentTasks").AsNumber())
}

// State written by the earlier buggy Update has an empty name and ID. Update
// and refresh repair it from the ID.
func TestEnvironment_EmptyNameStateIsRepaired(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Environment", "e")

	inputs := pm(map[string]property.Value{"name": str("staging"), "maxConcurrentTasks": num(10)})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)

	broken := pm(map[string]property.Value{"name": str(""), "environmentId": str(""), "isDefault": property.New(false), "maxConcurrentTasks": num(10)})
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: broken, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "staging", getStr(t, read.Properties, "name"))
	require.Equal(t, created.ID, getStr(t, read.Properties, "environmentId"))

	next := pm(map[string]property.Value{"name": str("staging"), "maxConcurrentTasks": num(15)})
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: broken, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "staging", getStr(t, upd.Properties, "name"))
	require.Equal(t, created.ID, getStr(t, upd.Properties, "environmentId"))
}
