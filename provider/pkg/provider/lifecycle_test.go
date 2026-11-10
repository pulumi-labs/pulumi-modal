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

// These tests drive every resource through the real provider RPC surface
// against an in-memory fake of Modal's gRPC API (see fake_modal_test.go).

func pm(kv map[string]property.Value) property.Map { return property.NewMap(kv) }

func str(s string) property.Value { return property.New(s) }

func strs(m map[string]string) property.Value {
	out := map[string]property.Value{}
	for k, v := range m {
		out[k] = property.New(v)
	}
	return property.New(out).WithSecret(true)
}

// serverFor returns a provider server configured against the fake at addr,
// with an optional provider-level environment.
func serverFor(t *testing.T, addr, environment string) integration.Server {
	t.Helper()
	for _, v := range []string{"MODAL_TOKEN_ID", "MODAL_TOKEN_SECRET", "MODAL_ENVIRONMENT", "MODAL_SERVER_ADDR"} {
		t.Setenv(v, "")
	}
	p, err := New()
	require.NoError(t, err)
	s, err := integration.NewServer(t.Context(), "modal", semver.MustParse("0.0.1"), integration.WithProvider(p))
	require.NoError(t, err)
	cfg := map[string]property.Value{
		"tokenId":     str("ak-test"),
		"tokenSecret": str("as-test"),
		"serverAddr":  str(addr),
	}
	if environment != "" {
		cfg["environment"] = str(environment)
	}
	require.NoError(t, s.Configure(pgp.ConfigureRequest{Args: pm(cfg)}))
	return s
}

// serverForEnvDefaults is serverFor without resetting MODAL_ENVIRONMENT or the config's
// environment, so the environment variable default is what applies.
func serverForEnvDefaults(t *testing.T, addr string) integration.Server {
	t.Helper()
	p, err := New()
	require.NoError(t, err)
	s, err := integration.NewServer(t.Context(), "modal", semver.MustParse("0.0.1"), integration.WithProvider(p))
	require.NoError(t, err)
	cfg := map[string]property.Value{
		"tokenId":     str("ak-test"),
		"tokenSecret": str("as-test"),
		"serverAddr":  str(addr),
	}
	require.NoError(t, s.Configure(pgp.ConfigureRequest{Args: pm(cfg)}))
	return s
}

func getStr(t *testing.T, m property.Map, key string) string {
	t.Helper()
	v, ok := m.GetOk(key)
	require.True(t, ok, "property %q missing from %v", key, m)
	return v.AsString()
}

func requireNoChanges(t *testing.T, d pgp.DiffResponse) {
	t.Helper()
	require.False(t, d.HasChanges, "unexpected diff: %+v", d.DetailedDiff)
}

// ---------------------------------------------------------------------------
// Volume
// ---------------------------------------------------------------------------

func TestVolume_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Volume", "vol")

	// Create without fsVersion: defaults to v2 and records the effective
	// environment (the account default, since nothing was configured).
	inputs := pm(map[string]property.Value{"name": str("data")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "v2", getStr(t, created.Properties, "fsVersion"))
	require.Equal(t, "main", getStr(t, created.Properties, "environment"))
	require.Equal(t, created.ID, getStr(t, created.Properties, "volumeId"))
	state := created.Properties

	// Regression: an explicit fsVersion that matches state must not produce a
	// diff (a property named "version" used to be dropped by the framework and
	// made every update replace the volume).
	for _, in := range []property.Map{
		pm(map[string]property.Value{"name": str("data"), "fsVersion": str("v2")}),
		pm(map[string]property.Value{"name": str("data")}),
		pm(map[string]property.Value{"name": str("data"), "fsVersion": str("v2"), "environment": str("main")}),
	} {
		d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: in, OldInputs: inputs})
		require.NoError(t, err)
		requireNoChanges(t, d)
	}

	// Refresh keeps the same shape.
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, created.ID, read.ID)
	require.Equal(t, "data", getStr(t, read.Properties, "name"))
	require.Equal(t, "v2", getStr(t, read.Properties, "fsVersion"))
	require.Equal(t, "main", getStr(t, read.Properties, "environment"))

	// Rename is an in-place update, not a replace.
	renamed := pm(map[string]property.Value{"name": str("data2"), "fsVersion": str("v2")})
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: renamed, OldInputs: inputs})
	require.NoError(t, err)
	require.True(t, d.HasChanges)
	require.Equal(t, pgp.Update, d.DetailedDiff["name"].Kind)
	require.False(t, d.DeleteBeforeReplace)
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: renamed, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "data2", getStr(t, upd.Properties, "name"))
	fake.mu.Lock()
	require.Equal(t, "data2", fake.volumes[created.ID].name)
	fake.mu.Unlock()
	state = upd.Properties

	// A refresh picks up a rename done outside Pulumi.
	fake.mu.Lock()
	fake.volumes[created.ID].name = "renamed-outside"
	fake.mu.Unlock()
	read, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: renamed})
	require.NoError(t, err)
	require.Equal(t, "renamed-outside", getStr(t, read.Properties, "name"))
	require.Equal(t, "renamed-outside", getStr(t, read.Inputs, "name"))

	// Changing the filesystem version replaces the volume, deleting the old
	// one first because the replacement keeps the same name and environment.
	v1 := pm(map[string]property.Value{"name": str("data2"), "fsVersion": str("v1")})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: v1, OldInputs: renamed})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["fsVersion"].Kind)
	require.True(t, d.DeleteBeforeReplace)

	// Delete, then a refresh reports the volume gone.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: renamed})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
	// Deleting something already gone is not an error.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
}

func TestVolume_EnvironmentChangeReplacesWithoutDeleteBeforeReplace(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "staging")
	urn := modalUrn("Volume", "vol")

	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: pm(map[string]property.Value{"name": str("data")})})
	require.NoError(t, err)
	require.Equal(t, "staging", getStr(t, created.Properties, "environment"))

	// Same provider environment: no diff.
	same := pm(map[string]property.Value{"name": str("data")})
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: same})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Pointing the provider at another environment replaces the resource; a
	// different environment cannot collide on name, so no delete-before-replace.
	s2 := serverFor(t, addr, "prod")
	d, err = s2.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: same})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["environment"].Kind)
	require.False(t, d.DeleteBeforeReplace)

	// So does an explicit resource-level environment.
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties,
		Inputs: pm(map[string]property.Value{"name": str("data"), "environment": str("prod")})})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["environment"].Kind)
}

func TestVolume_CheckRejectsUnknownFsVersion(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	resp, err := s.Check(pgp.CheckRequest{Urn: modalUrn("Volume", "vol"),
		Inputs: pm(map[string]property.Value{"name": str("data"), "fsVersion": str("v3")})})
	require.NoError(t, err)
	require.Len(t, resp.Failures, 1)
	require.Equal(t, "fsVersion", resp.Failures[0].Property)

	ok, err := s.Check(pgp.CheckRequest{Urn: modalUrn("Volume", "vol"),
		Inputs: pm(map[string]property.Value{"name": str("data"), "fsVersion": str("v1")})})
	require.NoError(t, err)
	require.Empty(t, ok.Failures)
}

func TestVolume_ImportFromIDAlone(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Volume", "vol")

	// A v1 volume created outside Pulumi, in the default environment.
	fake.mu.Lock()
	fake.volumes["vo-ext"] = &fakeObject{id: "vo-ext", name: "legacy", env: "main", version: 1}
	fake.mu.Unlock()

	read, err := s.Read(pgp.ReadRequest{ID: "vo-ext", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "vo-ext", read.ID)
	require.Equal(t, "legacy", getStr(t, read.Properties, "name"))
	require.Equal(t, "v1", getStr(t, read.Properties, "fsVersion"))
	require.Equal(t, "main", getStr(t, read.Properties, "environment"))
	require.Equal(t, "vo-ext", getStr(t, read.Properties, "volumeId"))
	require.Equal(t, "legacy", getStr(t, read.Inputs, "name"))
	require.Equal(t, "v1", getStr(t, read.Inputs, "fsVersion"))

	// The imported inputs diff clean against the imported state.
	d, err := s.Diff(pgp.DiffRequest{ID: "vo-ext", Urn: urn, State: read.Properties, Inputs: read.Inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// A volume in a different environment is reported, not silently adopted.
	fake.mu.Lock()
	fake.volumes["vo-other"] = &fakeObject{id: "vo-other", name: "elsewhere", env: "other", version: 2}
	fake.mu.Unlock()
	_, err = s.Read(pgp.ReadRequest{ID: "vo-other", Urn: urn})
	require.ErrorContains(t, err, "modal:environment")

	// With the environment configured the same import succeeds.
	sOther := serverFor(t, addr, "other")
	read, err = sOther.Read(pgp.ReadRequest{ID: "vo-other", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "other", getStr(t, read.Properties, "environment"))

	// An import of an unknown ID fails instead of storing an empty resource (#1715).
	_, err = s.Read(pgp.ReadRequest{ID: "vo-nope", Urn: urn})
	require.ErrorContains(t, err, "does not exist")
}

// TestVolume_LegacyVersionStateMigrates covers state written by v0.1.0, where
// the filesystem version lived under "version".
func TestVolume_LegacyVersionStateMigrates(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Volume", "vol")
	legacy := pm(map[string]property.Value{
		"name":     str("data"),
		"version":  str("v1"),
		"volumeId": str("vo-1"),
	})

	d, err := s.Diff(pgp.DiffRequest{ID: "vo-1", Urn: urn, State: legacy,
		Inputs: pm(map[string]property.Value{"name": str("data"), "fsVersion": str("v1")})})
	require.NoError(t, err)
	requireNoChanges(t, d)

	d, err = s.Diff(pgp.DiffRequest{ID: "vo-1", Urn: urn, State: legacy,
		Inputs: pm(map[string]property.Value{"name": str("data"), "fsVersion": str("v2")})})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["fsVersion"].Kind)
}

// ---------------------------------------------------------------------------
// Secret
// ---------------------------------------------------------------------------

func TestSecret_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Secret", "sec")

	inputs := pm(map[string]property.Value{
		"name":   str("creds"),
		"values": strs(map[string]string{"API_KEY": "one", "REGION": "us"}),
	})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, "main", getStr(t, state, "environment"))
	keys := state.Get("keys").AsArray()
	require.Equal(t, 2, keys.Len())
	require.Equal(t, "API_KEY", keys.Get(0).AsString())

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Refresh sees the same keys, so there is still no diff.
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	state = read.Properties
	require.Equal(t, "creds", getStr(t, state, "name"))
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Value change updates in place and drops keys that left the program.
	next := pm(map[string]property.Value{
		"name":   str("creds"),
		"values": strs(map[string]string{"API_KEY": "two"}),
	})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["values"].Kind)
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	state = upd.Properties
	fake.mu.Lock()
	require.Equal(t, map[string]string{"API_KEY": "two"}, fake.secrets[created.ID].values)
	// Drift: a key added outside Pulumi.
	fake.secrets[created.ID].values["EXTRA"] = "x"
	fake.mu.Unlock()

	read, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: next})
	require.NoError(t, err)
	state = read.Properties
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: next, OldInputs: next})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["values"].Kind, "drifted keys must show up as a values update")
	_, err = s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: next, OldInputs: next})
	require.NoError(t, err)
	fake.mu.Lock()
	require.Equal(t, map[string]string{"API_KEY": "two"}, fake.secrets[created.ID].values)
	fake.mu.Unlock()

	// Name change replaces (Modal has no rename for secrets).
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state,
		Inputs: pm(map[string]property.Value{"name": str("other"), "values": strs(map[string]string{"API_KEY": "two"})})})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["name"].Kind)

	// Delete, then refresh reports it gone.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: next})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

func TestSecret_CheckRejectsInvalidKeys(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	resp, err := s.Check(pgp.CheckRequest{Urn: modalUrn("Secret", "sec"), Inputs: pm(map[string]property.Value{
		"name":   str("creds"),
		"values": strs(map[string]string{"1BAD": "x", "bad-key": "y", "GOOD_KEY": "z"}),
	})})
	require.NoError(t, err)
	require.Len(t, resp.Failures, 2)
	for _, f := range resp.Failures {
		require.Equal(t, "values", f.Property)
	}
}

func TestSecret_ImportFromIDAlone(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Secret", "sec")
	fake.mu.Lock()
	fake.secrets["st-ext"] = &fakeObject{id: "st-ext", name: "imported", env: "other", values: map[string]string{"B": "2", "A": "1"}}
	fake.mu.Unlock()

	read, err := s.Read(pgp.ReadRequest{ID: "st-ext", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "st-ext", read.ID)
	require.Equal(t, "imported", getStr(t, read.Properties, "name"))
	// The environment comes from the secret's own metadata.
	require.Equal(t, "other", getStr(t, read.Properties, "environment"))
	require.Equal(t, "other", getStr(t, read.Inputs, "environment"))
	keys := read.Properties.Get("keys").AsArray()
	require.Equal(t, "A", keys.Get(0).AsString())
	require.Equal(t, "B", keys.Get(1).AsString())

	// Values are unknown after import, so a program with the same keys diffs clean (#1515).
	prog := pm(map[string]property.Value{"name": str("imported"), "values": strs(map[string]string{"A": "1", "B": "2"})})
	d, err := s.Diff(pgp.DiffRequest{ID: "st-ext", Urn: urn, State: read.Properties, Inputs: prog, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// A key added or removed relative to Modal is still an update.
	extra := pm(map[string]property.Value{"name": str("imported"), "values": strs(map[string]string{"A": "1", "B": "2", "C": "3"})})
	d, err = s.Diff(pgp.DiffRequest{ID: "st-ext", Urn: urn, State: read.Properties, Inputs: extra, OldInputs: read.Inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["values"].Kind)
	missing := pm(map[string]property.Value{"name": str("imported"), "values": strs(map[string]string{"A": "1"})})
	d, err = s.Diff(pgp.DiffRequest{ID: "st-ext", Urn: urn, State: read.Properties, Inputs: missing, OldInputs: read.Inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["values"].Kind)

	// After the first up the engine records the program's inputs; a later value
	// change is detected through the old inputs even though state values stay empty.
	changed := pm(map[string]property.Value{"name": str("imported"), "values": strs(map[string]string{"A": "1", "B": "changed"})})
	d, err = s.Diff(pgp.DiffRequest{ID: "st-ext", Urn: urn, State: read.Properties, Inputs: changed, OldInputs: prog})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["values"].Kind)
	d, err = s.Diff(pgp.DiffRequest{ID: "st-ext", Urn: urn, State: read.Properties, Inputs: prog, OldInputs: prog})
	require.NoError(t, err)
	requireNoChanges(t, d)
}

// ---------------------------------------------------------------------------
// Queue
// ---------------------------------------------------------------------------

func TestQueue_Lifecycle(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Queue", "q")

	inputs := pm(map[string]property.Value{"name": str("jobs")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, "main", getStr(t, state, "environment"))

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "jobs", getStr(t, read.Properties, "name"))

	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state,
		Inputs: pm(map[string]property.Value{"name": str("jobs2")})})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["name"].Kind)

	d, err = serverFor(t, addr, "prod").Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["environment"].Kind)

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

func TestQueue_ImportFromIDAlone(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Queue", "q")
	fake.mu.Lock()
	fake.queues["qu-ext"] = &fakeObject{id: "qu-ext", name: "existing", env: "main"}
	fake.queues["qu-other"] = &fakeObject{id: "qu-other", name: "elsewhere", env: "other"}
	fake.mu.Unlock()

	read, err := s.Read(pgp.ReadRequest{ID: "qu-ext", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "existing", getStr(t, read.Properties, "name"))
	require.Equal(t, "main", getStr(t, read.Properties, "environment"))
	d, err := s.Diff(pgp.DiffRequest{ID: "qu-ext", Urn: urn, State: read.Properties, Inputs: read.Inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	_, err = s.Read(pgp.ReadRequest{ID: "qu-other", Urn: urn})
	require.ErrorContains(t, err, "modal:environment")
}

// TestAuthentication verifies the provider sends the static token headers the
// fake enforces: wrong credentials fail every resource's create.
func TestAuthentication(t *testing.T) {
	_, addr := startFakeModal(t)
	for _, v := range []string{"MODAL_TOKEN_ID", "MODAL_TOKEN_SECRET", "MODAL_ENVIRONMENT", "MODAL_SERVER_ADDR"} {
		t.Setenv(v, "")
	}
	p, err := New()
	require.NoError(t, err)
	s, err := integration.NewServer(t.Context(), "modal", semver.MustParse("0.0.1"), integration.WithProvider(p))
	require.NoError(t, err)
	require.NoError(t, s.Configure(pgp.ConfigureRequest{Args: pm(map[string]property.Value{
		"tokenId": str("wrong"), "tokenSecret": str("as-test"), "serverAddr": str(addr),
	})}))
	_, err = s.Create(pgp.CreateRequest{Urn: modalUrn("Queue", "q"), Properties: pm(map[string]property.Value{"name": str("jobs")})})
	require.Error(t, err)
}
