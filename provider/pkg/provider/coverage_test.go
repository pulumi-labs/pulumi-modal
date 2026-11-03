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
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

// Lifecycle and lookup tests for the Dict, Environment, App and Image
// resources and the get* functions, against the in-memory fake of Modal.

func num(n int) property.Value { return property.New(float64(n)) }

func strArray(vals ...string) property.Value {
	arr := make([]property.Value, 0, len(vals))
	for _, v := range vals {
		arr = append(arr, property.New(v))
	}
	return property.New(property.NewArray(arr))
}

func strMap(kv map[string]string) property.Value {
	m := map[string]property.Value{}
	for k, v := range kv {
		m[k] = property.New(v)
	}
	return property.New(property.NewMap(m))
}

// ---------------------------------------------------------------------------
// Dict
// ---------------------------------------------------------------------------

func TestDict_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Dict", "d")

	inputs := pm(map[string]property.Value{"name": str("cache")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, "main", getStr(t, state, "environment"))
	require.Equal(t, created.ID, getStr(t, state, "dictId"))

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "cache", getStr(t, read.Properties, "name"))

	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: pm(map[string]property.Value{"name": str("cache2")})})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["name"].Kind)
	d, err = serverFor(t, addr, "other").Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["environment"].Kind)

	// Import by ID alone, and the wrong-environment message.
	fake.mu.Lock()
	fake.dicts["di-ext"] = &fakeObject{id: "di-ext", name: "legacy", env: "other"}
	fake.mu.Unlock()
	_, err = s.Read(pgp.ReadRequest{ID: "di-ext", Urn: urn})
	require.ErrorContains(t, err, "modal:environment")
	imp, err := serverFor(t, addr, "other").Read(pgp.ReadRequest{ID: "di-ext", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "legacy", getStr(t, imp.Inputs, "name"))
	require.Equal(t, "other", getStr(t, imp.Properties, "environment"))

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

// ---------------------------------------------------------------------------
// Environment
// ---------------------------------------------------------------------------

func TestEnvironment_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Environment", "e")

	inputs := pm(map[string]property.Value{"name": str("staging"), "maxConcurrentTasks": num(10)})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, "env-staging", created.ID)
	require.Equal(t, "env-staging", getStr(t, state, "environmentId"))
	require.Equal(t, "auto-staging", getStr(t, state, "webSuffix"), "the server-chosen suffix is reported")
	require.Equal(t, float64(10), state.Get("maxConcurrentTasks").AsNumber())
	require.False(t, state.Get("isDefault").AsBool())

	// Unset webSuffix input does not diff against the computed one.
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Rename and limit changes are in-place updates.
	next := pm(map[string]property.Value{"name": str("staging2"), "maxConcurrentTasks": num(20), "webSuffix": str("stg")})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["name"].Kind)
	require.Equal(t, pgp.Update, d.DetailedDiff["webSuffix"].Kind)
	require.Equal(t, pgp.Update, d.DetailedDiff["maxConcurrentTasks"].Kind)
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	state = upd.Properties
	require.Equal(t, "staging2", getStr(t, state, "name"))
	fake.mu.Lock()
	require.Equal(t, "staging2", fake.envs[created.ID].name)
	require.Equal(t, "stg", fake.envs[created.ID].suffix)
	require.Equal(t, int32(20), *fake.envs[created.ID].maxTasks)
	// Drift: renamed outside Pulumi.
	fake.envs[created.ID].name = "renamed-outside"
	fake.mu.Unlock()

	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: next})
	require.NoError(t, err)
	require.Equal(t, "renamed-outside", getStr(t, read.Properties, "name"))
	require.Equal(t, "renamed-outside", getStr(t, read.Inputs, "name"))

	// Import by ID alone adopts what Modal reports.
	imp, err := s.Read(pgp.ReadRequest{ID: "env-other", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "other", getStr(t, imp.Inputs, "name"))
	require.Equal(t, "other-suffix", getStr(t, imp.Inputs, "webSuffix"))

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: read.Properties}))
	fake.mu.Lock()
	_, still := fake.envs[created.ID]
	fake.mu.Unlock()
	require.False(t, still)
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: next})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
	// Deleting what is already gone is not an error.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
}

// ---------------------------------------------------------------------------
// App
// ---------------------------------------------------------------------------

func TestApp_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("App", "a")

	inputs := pm(map[string]property.Value{"name": str("trainer"), "tags": strMap(map[string]string{"team": "ml"})})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, "main", getStr(t, state, "environment"))
	require.Equal(t, created.ID, getStr(t, state, "appId"))
	fake.mu.Lock()
	require.Equal(t, map[string]string{"team": "ml"}, fake.apps[created.ID].tags)
	fake.mu.Unlock()

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Refresh reads name, environment and tags by ID.
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "trainer", getStr(t, read.Properties, "name"))
	require.Equal(t, "ml", read.Properties.Get("tags").AsMap().Get("team").AsString())

	// Tag changes update in place.
	next := pm(map[string]property.Value{"name": str("trainer"), "tags": strMap(map[string]string{"team": "ml", "tier": "gold"})})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["tags"].Kind)
	_, err = s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	fake.mu.Lock()
	require.Equal(t, "gold", fake.apps[created.ID].tags["tier"])
	fake.mu.Unlock()

	// Name and environment changes replace.
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: pm(map[string]property.Value{"name": str("other"), "tags": strMap(map[string]string{"team": "ml"})})})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["name"].Kind)
	d, err = serverFor(t, addr, "other").Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["environment"].Kind)

	// Import by ID alone reads the environment from the app.
	fake.mu.Lock()
	fake.apps["ap-ext"] = &fakeApp{id: "ap-ext", name: "deployed", env: "other", tags: map[string]string{"a": "b"}}
	fake.mu.Unlock()
	imp, err := s.Read(pgp.ReadRequest{ID: "ap-ext", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "deployed", getStr(t, imp.Inputs, "name"))
	require.Equal(t, "other", getStr(t, imp.Inputs, "environment"))
	require.Equal(t, "b", imp.Inputs.Get("tags").AsMap().Get("a").AsString())

	// Delete stops the app; refresh then reports it gone.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

// ---------------------------------------------------------------------------
// Image
// ---------------------------------------------------------------------------

func TestImage_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")

	inputs := pm(map[string]property.Value{
		"baseImage":          str("python:3.12-slim"),
		"dockerfileCommands": strArray("RUN pip install torch"),
		"name":               str("torch-base"),
	})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, created.ID, getStr(t, state, "imageId"))
	require.Equal(t, "main", getStr(t, state, "environment"))
	fake.mu.Lock()
	img := fake.images[created.ID]
	// The base image is built as its own layer; the commands layer builds on it.
	require.Equal(t, []string{"FROM base", "RUN pip install torch"}, img.commands)
	var layerBases []string
	for _, other := range fake.images {
		if other.id != created.ID && len(other.commands) == 1 {
			layerBases = append(layerBases, other.commands[0])
		}
	}
	require.Equal(t, []string{"FROM python:3.12-slim"}, layerBases)
	require.Equal(t, created.ID, fake.tags["main/torch-base:latest"], "image is published under its name")
	fake.mu.Unlock()

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, created.ID, read.ID)
	require.Equal(t, "torch-base", getStr(t, read.Properties, "name"))

	// A build input change replaces.
	changed := pm(map[string]property.Value{
		"baseImage":          str("python:3.12-slim"),
		"dockerfileCommands": strArray("RUN pip install jax"),
		"name":               str("torch-base"),
	})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: changed, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["dockerfileCommands"].Kind)

	// A name change republishes in place and removes the old name.
	renamed := pm(map[string]property.Value{
		"baseImage":          str("python:3.12-slim"),
		"dockerfileCommands": strArray("RUN pip install torch"),
		"name":               str("torch:v2"),
	})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: renamed, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["name"].Kind)
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: renamed, OldInputs: inputs})
	require.NoError(t, err)
	state = upd.Properties
	fake.mu.Lock()
	require.Equal(t, created.ID, fake.tags["main/torch:v2"])
	require.NotContains(t, fake.tags, "main/torch-base:latest")
	fake.mu.Unlock()

	// A name removed outside Pulumi drops out of state on refresh.
	fake.mu.Lock()
	delete(fake.tags, "main/torch:v2")
	fake.mu.Unlock()
	read, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: renamed})
	require.NoError(t, err)
	_, hasName := read.Properties.GetOk("name")
	require.False(t, hasName)

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: renamed})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

func TestImage_RegistrySecretAndGpuReachTheBuild(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	secret, err := s.Create(pgp.CreateRequest{Urn: modalUrn("Secret", "reg"), Properties: pm(map[string]property.Value{
		"name": str("ghcr"), "values": strs(map[string]string{"REGISTRY_USERNAME": "u", "REGISTRY_PASSWORD": "p"}),
	})})
	require.NoError(t, err)

	created, err := s.Create(pgp.CreateRequest{Urn: modalUrn("Image", "img"), Properties: pm(map[string]property.Value{
		"baseImage":          str("ghcr.io/acme/base:1"),
		"registrySecret":     str("ghcr"),
		"dockerfileCommands": strArray("RUN echo hi"),
		"gpu":                str("T4"),
	})})
	require.NoError(t, err)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.True(t, fake.images[created.ID].gpu)
	_ = secret
}

func TestImage_ImportAdoptsTheProgram(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")
	fake.mu.Lock()
	fake.images["im-ext"] = &fakeImage{id: "im-ext", env: "main"}
	fake.tags["main/shared:prod"] = "im-ext"
	fake.mu.Unlock()

	imp, err := s.Read(pgp.ReadRequest{ID: "im-ext", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "im-ext", imp.ID)
	require.Equal(t, "shared:prod", getStr(t, imp.Inputs, "name"))
	require.Equal(t, "main", getStr(t, imp.Properties, "environment"))
	_, hasBase := imp.Properties.GetOk("baseImage")
	require.False(t, hasBase, "Modal does not store what an image was built from")

	// The imported image adopts whatever build inputs the program declares.
	prog := pm(map[string]property.Value{
		"baseImage": str("python:3.12"), "dockerfileCommands": strArray("RUN true"), "name": str("shared:prod"),
	})
	d, err := s.Diff(pgp.DiffRequest{ID: "im-ext", Urn: urn, State: imp.Properties, Inputs: prog, OldInputs: imp.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	missing, err := s.Read(pgp.ReadRequest{ID: "im-nope", Urn: urn})
	require.NoError(t, err)
	require.Empty(t, missing.ID)
}

func TestImage_CheckAndCreateValidation(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")
	for _, bad := range []string{"", "env/name", "im-123"} {
		resp, err := s.Check(pgp.CheckRequest{Urn: urn, Inputs: pm(map[string]property.Value{
			"baseImage": str("python:3.12"), "name": str(bad),
		})})
		require.NoError(t, err)
		require.Len(t, resp.Failures, 1, "name %q", bad)
		require.Equal(t, "name", resp.Failures[0].Property)
	}
	_, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: pm(map[string]property.Value{"name": str("x")})})
	require.ErrorContains(t, err, "baseImage is required")
}

// ---------------------------------------------------------------------------
// Lookup functions
// ---------------------------------------------------------------------------

func invoke(t *testing.T, s interface {
	Invoke(pgp.InvokeRequest) (pgp.InvokeResponse, error)
}, token string, args map[string]property.Value) (property.Map, error) {
	t.Helper()
	resp, err := s.Invoke(pgp.InvokeRequest{Token: tokens.Type("modal:index:" + token), Args: pm(args)})
	if err != nil {
		return property.Map{}, err
	}
	return resp.Return, nil
}

func TestFunctions_LookUpExistingObjects(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	fake.mu.Lock()
	fake.secrets["st-1"] = &fakeObject{id: "st-1", name: "creds", env: "main", values: map[string]string{"B": "2", "A": "1"}}
	fake.volumes["vo-1"] = &fakeObject{id: "vo-1", name: "weights", env: "main", version: 2}
	fake.queues["qu-1"] = &fakeObject{id: "qu-1", name: "jobs", env: "main"}
	fake.dicts["di-1"] = &fakeObject{id: "di-1", name: "state", env: "main"}
	fake.apps["ap-1"] = &fakeApp{id: "ap-1", name: "deployed-app", env: "main", tags: map[string]string{"k": "v"}}
	fake.images["im-1"] = &fakeImage{id: "im-1", env: "main"}
	fake.tags["main/base:latest"] = "im-1"
	fake.mu.Unlock()

	out, err := invoke(t, s, "getSecret", map[string]property.Value{"name": str("creds")})
	require.NoError(t, err)
	require.Equal(t, "st-1", getStr(t, out, "secretId"))
	require.Equal(t, "A", out.Get("keys").AsArray().Get(0).AsString())
	require.Equal(t, "main", getStr(t, out, "environment"))

	out, err = invoke(t, s, "getVolume", map[string]property.Value{"name": str("weights")})
	require.NoError(t, err)
	require.Equal(t, "vo-1", getStr(t, out, "volumeId"))
	require.Equal(t, "v2", getStr(t, out, "fsVersion"))

	out, err = invoke(t, s, "getQueue", map[string]property.Value{"name": str("jobs")})
	require.NoError(t, err)
	require.Equal(t, "qu-1", getStr(t, out, "queueId"))

	out, err = invoke(t, s, "getDict", map[string]property.Value{"name": str("state")})
	require.NoError(t, err)
	require.Equal(t, "di-1", getStr(t, out, "dictId"))

	out, err = invoke(t, s, "getEnvironment", map[string]property.Value{"name": str("main")})
	require.NoError(t, err)
	require.Equal(t, "env-main", getStr(t, out, "environmentId"))
	require.True(t, out.Get("isDefault").AsBool())

	out, err = invoke(t, s, "getApp", map[string]property.Value{"name": str("deployed-app")})
	require.NoError(t, err)
	require.Equal(t, "ap-1", getStr(t, out, "appId"))
	require.Equal(t, "v", out.Get("tags").AsMap().Get("k").AsString())

	out, err = invoke(t, s, "getImage", map[string]property.Value{"name": str("base")})
	require.NoError(t, err)
	require.Equal(t, "im-1", getStr(t, out, "imageId"))
	require.Equal(t, "base:latest", getStr(t, out, "name"))

	out, err = invoke(t, s, "getFunction", map[string]property.Value{"appName": str("deployed-app"), "functionName": str("predict")})
	require.NoError(t, err)
	require.Equal(t, "fu-1", getStr(t, out, "functionId"))
	require.Equal(t, "https://example--predict.modal.run", getStr(t, out, "webUrl"))

	out, err = invoke(t, s, "getCls", map[string]property.Value{"appName": str("deployed-app"), "clsName": str("Model")})
	require.NoError(t, err)
	require.Equal(t, "cs-1", getStr(t, out, "clsId"))
	require.Equal(t, "infer", out.Get("methods").AsArray().Get(0).AsString())

	out, err = invoke(t, s, "getProxy", map[string]property.Value{"name": str("egress")})
	require.NoError(t, err)
	require.Equal(t, "pr-1", getStr(t, out, "proxyId"))
	require.Equal(t, "203.0.113.1", out.Get("ips").AsArray().Get(0).AsString())

	out, err = invoke(t, s, "getWorkspace", map[string]property.Value{})
	require.NoError(t, err)
	require.Equal(t, "acme", getStr(t, out, "workspaceName"))
	require.Equal(t, "main", getStr(t, out, "defaultEnvironment"))
}

func TestFunctions_MissingObjectsNameTheObjectAndEnvironment(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	for _, tc := range []struct {
		token string
		args  map[string]property.Value
		want  string
	}{
		{"getSecret", map[string]property.Value{"name": str("nope")}, `secret "nope"`},
		{"getVolume", map[string]property.Value{"name": str("nope")}, `volume "nope"`},
		{"getQueue", map[string]property.Value{"name": str("nope")}, `queue "nope"`},
		{"getDict", map[string]property.Value{"name": str("nope")}, `dict "nope"`},
		{"getApp", map[string]property.Value{"name": str("nope")}, `app "nope"`},
		{"getImage", map[string]property.Value{"name": str("nope")}, `image "nope:latest"`},
		{"getEnvironment", map[string]property.Value{"name": str("nope")}, `environment "nope"`},
		{"getFunction", map[string]property.Value{"appName": str("a"), "functionName": str("f")}, `function "a.f"`},
		{"getCls", map[string]property.Value{"appName": str("a"), "clsName": str("C")}, `cls "a.C"`},
		{"getProxy", map[string]property.Value{"name": str("nope")}, `proxy "nope"`},
	} {
		_, err := invoke(t, s, tc.token, tc.args)
		require.ErrorContains(t, err, tc.want, tc.token)
	}
	_, err := invoke(t, serverFor(t, addr, "other"), "getQueue", map[string]property.Value{"name": str("nope")})
	require.ErrorContains(t, err, `environment "other"`)
}

func TestFunctions_EnvironmentArgumentOverridesProviderEnvironment(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "main")
	fake.mu.Lock()
	fake.queues["qu-o"] = &fakeObject{id: "qu-o", name: "jobs", env: "other"}
	fake.mu.Unlock()
	out, err := invoke(t, s, "getQueue", map[string]property.Value{"name": str("jobs"), "environment": str("other")})
	require.NoError(t, err)
	require.Equal(t, "qu-o", getStr(t, out, "queueId"))
}

// TestImage_NameSpellingsOfTheSameTagAreNoChange: "foo" and "foo:latest" are one
// published name, so neither Diff nor Update may republish (publishing then
// deleting the same tag would unpublish the image).
func TestImage_NameSpellingsOfTheSameTagAreNoChange(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")
	inputs := pm(map[string]property.Value{"baseImage": str("python:3.12"), "name": str("foo")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties

	respelled := pm(map[string]property.Value{"baseImage": str("python:3.12"), "name": str("foo:latest")})
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: respelled, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Even if Update is called with the respelled name, the tag stays published.
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: respelled, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "foo:latest", getStr(t, upd.Properties, "name"))
	fake.mu.Lock()
	require.Equal(t, created.ID, fake.tags["main/foo:latest"])
	fake.mu.Unlock()

	// A real rename still moves the tag.
	moved := pm(map[string]property.Value{"baseImage": str("python:3.12"), "name": str("bar")})
	_, err = s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: upd.Properties, Inputs: moved, OldInputs: respelled})
	require.NoError(t, err)
	fake.mu.Lock()
	require.Equal(t, created.ID, fake.tags["main/bar:latest"])
	require.NotContains(t, fake.tags, "main/foo:latest")
	fake.mu.Unlock()
}

// TestImage_FailedPublishLeavesPartialStateWithoutName: when the build works
// but publishing fails, the image is recorded without its name, so the next
// Diff reports the name as a change and Update publishes it.
func TestImage_FailedPublishLeavesPartialStateWithoutName(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")
	inputs := pm(map[string]property.Value{"baseImage": str("python:3.12"), "name": str("foo")})
	fake.mu.Lock()
	fake.failPublish = true
	fake.mu.Unlock()
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.Error(t, err)
	require.NotEmpty(t, created.ID, "the built image must be tracked, not leaked")
	require.NotNil(t, created.PartialState)
	_, hasName := created.Properties.GetOk("name")
	require.False(t, hasName)

	fake.mu.Lock()
	fake.failPublish = false
	fake.mu.Unlock()
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["name"].Kind)
	_, err = s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	fake.mu.Lock()
	require.Equal(t, created.ID, fake.tags["main/foo:latest"])
	fake.mu.Unlock()
}

// TestApp_FailedTagsLeavesPartialStateWithoutTags is the same guarantee for App.
func TestApp_FailedTagsLeavesPartialStateWithoutTags(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("App", "a")
	inputs := pm(map[string]property.Value{"name": str("trainer"), "tags": strMap(map[string]string{"team": "ml"})})
	fake.mu.Lock()
	fake.failSetTags = true
	fake.mu.Unlock()
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.Error(t, err)
	require.NotEmpty(t, created.ID)
	require.NotNil(t, created.PartialState)
	_, hasTags := created.Properties.GetOk("tags")
	require.False(t, hasTags)

	fake.mu.Lock()
	fake.failSetTags = false
	fake.mu.Unlock()
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["tags"].Kind)
}
