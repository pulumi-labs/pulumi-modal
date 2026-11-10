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

func wsIn(env, builder string) property.Map {
	m := map[string]property.Value{}
	if env != "" {
		m["defaultEnvironment"] = str(env)
	}
	if builder != "" {
		m["imageBuilderVersion"] = str(builder)
	}
	return pm(m)
}

// #1537: the workspace settings singleton sets the default environment and the
// image builder version in place, reads them back, and leaves an unset one alone.
func TestWorkspaceSettings_Lifecycle(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("WorkspaceSettings", "ws")
	_, err := s.Create(pgp.CreateRequest{Urn: modalUrn("Environment", "e"), Properties: pm(map[string]property.Value{"name": str("staging")})})
	require.NoError(t, err)

	in := wsIn("staging", "2025.06")
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.NoError(t, err)
	require.Equal(t, "ac-1", created.ID, "the ID is the workspace ID")
	require.Equal(t, "ac-1", created.Properties.Get("workspaceId").AsString())
	require.Equal(t, "staging", f.wsDefaultEnv)
	require.Equal(t, "2025.06", f.wsBuilder)

	// A refresh reads both back and nothing diffs.
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: in})
	require.NoError(t, err)
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: in, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Drift made in the Modal dashboard shows as an update of that setting only.
	f.wsBuilder = "2024.10"
	read, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: in})
	require.NoError(t, err)
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: in, OldInputs: read.Inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["imageBuilderVersion"].Kind)
	require.NotContains(t, d.DetailedDiff, "defaultEnvironment")

	// An update calls only the setting that changed.
	f.wsSetCalls = nil
	next := wsIn("staging", "2025.12")
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: next, OldInputs: in})
	require.NoError(t, err)
	require.Equal(t, []string{"builder"}, f.wsSetCalls)
	require.Equal(t, "2025.12", f.wsBuilder)
	require.Equal(t, "2025.12", upd.Properties.Get("imageBuilderVersion").AsString())

	// An unset setting is left alone, even though the state has a value for it.
	onlyEnv := wsIn("staging", "")
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: upd.Properties, Inputs: onlyEnv, OldInputs: next})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Destroy leaves the workspace as it is.
	f.wsSetCalls = nil
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: upd.Properties}))
	require.Empty(t, f.wsSetCalls)
	require.Equal(t, "2025.12", f.wsBuilder)
}

func TestWorkspaceSettings_Import(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	f.wsDefaultEnv, f.wsBuilder = "main", "2025.06"
	imp, err := s.Read(pgp.ReadRequest{ID: "ac-1", Urn: modalUrn("WorkspaceSettings", "ws"), Properties: property.NewMap(nil), Inputs: property.NewMap(nil)})
	require.NoError(t, err)
	require.Equal(t, "ac-1", imp.ID)
	require.Equal(t, "main", imp.Inputs.Get("defaultEnvironment").AsString())
	require.Equal(t, "2025.06", imp.Inputs.Get("imageBuilderVersion").AsString())
	require.Equal(t, "ac-1", imp.Properties.Get("workspaceId").AsString())

	// An ID of another workspace is not visible to this token.
	_, err = s.Read(pgp.ReadRequest{ID: "ac-other", Urn: modalUrn("WorkspaceSettings", "ws"), Properties: property.NewMap(nil), Inputs: property.NewMap(nil)})
	require.ErrorContains(t, err, "does not exist")

	// A refresh of state created for another workspace drops it.
	state := pm(map[string]property.Value{"workspaceId": str("ac-other"), "defaultEnvironment": str("main")})
	gone, err := s.Read(pgp.ReadRequest{ID: "ac-other", Urn: modalUrn("WorkspaceSettings", "ws"), Properties: state, Inputs: wsIn("main", "")})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

func TestWorkspaceSettings_CheckAndFailures(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("WorkspaceSettings", "ws")

	chk, err := s.Check(pgp.CheckRequest{Urn: urn, Inputs: property.NewMap(nil)})
	require.NoError(t, err)
	require.NotEmpty(t, chk.Failures, "at least one setting is required")
	chk, err = s.Check(pgp.CheckRequest{Urn: urn, Inputs: wsIn("", "").Set("imageBuilderVersion", str(""))})
	require.NoError(t, err)
	require.NotEmpty(t, chk.Failures, "an empty string is rejected")
	chk, err = s.Check(pgp.CheckRequest{Urn: urn, Inputs: pm(map[string]property.Value{"defaultEnvironment": property.New(property.Computed)})})
	require.NoError(t, err)
	require.Empty(t, chk.Failures, "a computed setting counts as set")

	// A default environment that does not exist is an error from Modal and changes nothing.
	_, err = s.Create(pgp.CreateRequest{Urn: urn, Properties: wsIn("nope", "")})
	require.ErrorContains(t, err, "default environment")
	require.Empty(t, f.wsDefaultEnv)
}

func TestGetWorkspaceSettings(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	f.wsDefaultEnv, f.wsBuilder = "staging", "2025.06"
	out, err := invoke(t, s, "getWorkspaceSettings", map[string]property.Value{})
	require.NoError(t, err)
	require.Equal(t, "ac-1", out.Get("workspaceId").AsString())
	require.Equal(t, "staging", out.Get("defaultEnvironment").AsString())
	require.Equal(t, "2025.06", out.Get("imageBuilderVersion").AsString())
}

// A second call that fails leaves the first applied, returns an error and does not
// advance state; a retry then sets only what is still different.
func TestWorkspaceSettings_PartialFailure(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("WorkspaceSettings", "ws")
	_, err := s.Create(pgp.CreateRequest{Urn: modalUrn("Environment", "e"), Properties: pm(map[string]property.Value{"name": str("staging")})})
	require.NoError(t, err)

	f.wsBuilderErr = true
	in := wsIn("staging", "bogus")
	_, err = s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.ErrorContains(t, err, "image builder version")
	require.Equal(t, "staging", f.wsDefaultEnv, "the first setting stays applied")

	f.wsBuilderErr = false
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: wsIn("staging", "2025.06")})
	require.NoError(t, err)

	// A failing update returns an error and the retry only repeats the failed setting.
	f.wsBuilderErr = true
	next := wsIn("main", "2025.12")
	_, err = s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: next, OldInputs: wsIn("staging", "2025.06")})
	require.ErrorContains(t, err, "image builder version")
	require.Equal(t, "main", f.wsDefaultEnv)
	require.Equal(t, "2025.06", f.wsBuilder)
	f.wsBuilderErr = false
	f.wsSetCalls = nil
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: next, OldInputs: wsIn("staging", "2025.06")})
	require.NoError(t, err)
	require.True(t, d.HasChanges)
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: next})
	require.NoError(t, err)
	_, err = s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: next, OldInputs: wsIn("staging", "2025.06")})
	require.NoError(t, err)
	require.Equal(t, []string{"builder"}, f.wsSetCalls)
	require.Equal(t, "2025.12", f.wsBuilder)
}
