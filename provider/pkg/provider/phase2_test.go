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
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

// Lifecycle and lookup tests for the ServiceUser, Proxy and AppDeployment
// resources against the in-memory fake of Modal.

func strList(m property.Map, key string) []string {
	v, ok := m.GetOk(key)
	if !ok || !v.IsArray() {
		return nil
	}
	var out []string
	for _, e := range v.AsArray().AsSlice() {
		out = append(out, e.AsString())
	}
	return out
}

// ---------------------------------------------------------------------------
// ServiceUser
// ---------------------------------------------------------------------------

func TestServiceUser_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("ServiceUser", "ci")

	inputs := pm(map[string]property.Value{"name": str("ci-bot"), "scoped": property.New(true), "expiresInSeconds": num(3600)})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, created.ID, getStr(t, state, "serviceUserId"))
	require.Equal(t, "ak-"+created.ID, getStr(t, state, "serviceTokenId"))
	require.Equal(t, "as-secret-"+created.ID, getStr(t, state, "serviceTokenSecret"))
	require.NotEmpty(t, getStr(t, state, "expiresAt"))

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Every input is replace-only: Modal has no update call.
	for field, next := range map[string]map[string]property.Value{
		"name":             {"name": str("ci-bot2"), "scoped": property.New(true), "expiresInSeconds": num(3600)},
		"scoped":           {"name": str("ci-bot"), "scoped": property.New(false), "expiresInSeconds": num(3600)},
		"expiresInSeconds": {"name": str("ci-bot"), "scoped": property.New(true), "expiresInSeconds": num(60)},
	} {
		d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: pm(next), OldInputs: inputs})
		require.NoError(t, err)
		require.Equal(t, pgp.UpdateReplace, d.DetailedDiff[field].Kind, field)
	}

	// Read keeps the write-once secret.
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "as-secret-"+created.ID, getStr(t, read.Properties, "serviceTokenSecret"))
	require.Equal(t, "ci-bot", getStr(t, read.Inputs, "name"))
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Import by ID alone: no secret is recoverable.
	imp, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "ci-bot", getStr(t, imp.Inputs, "name"))
	require.Equal(t, "ak-"+created.ID, getStr(t, imp.Properties, "serviceTokenId"))
	_, hasSecret := imp.Properties.GetOk("serviceTokenSecret")
	require.False(t, hasSecret)

	// Deleted outside Pulumi.
	fake.mu.Lock()
	delete(fake.serviceUsers, created.ID)
	fake.mu.Unlock()
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)

	// Delete is idempotent once the user is gone, and removes a live one.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	again, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: pm(map[string]property.Value{"name": str("plain")})})
	require.NoError(t, err)
	_, hasExpiry := again.Properties.GetOk("expiresAt")
	require.False(t, hasExpiry)
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: again.ID, Urn: urn, Properties: again.Properties}))
	fake.mu.Lock()
	require.Empty(t, fake.serviceUsers)
	fake.mu.Unlock()
}

func TestServiceUser_CreateErrors(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	in := pm(map[string]property.Value{"name": str("dup")})
	_, err := s.Create(pgp.CreateRequest{Urn: modalUrn("ServiceUser", "a"), Properties: in})
	require.NoError(t, err)
	_, err = s.Create(pgp.CreateRequest{Urn: modalUrn("ServiceUser", "b"), Properties: in})
	require.ErrorContains(t, err, `create service user "dup"`)
	_, err = s.Create(pgp.CreateRequest{Urn: modalUrn("ServiceUser", "c"), Properties: pm(map[string]property.Value{
		"name": str("neg"), "expiresInSeconds": property.New(-5.0),
	})})
	require.ErrorContains(t, err, "must not be negative")
}

func TestGetServiceUser(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	fake.mu.Lock()
	fake.serviceUsers["su-1"] = &fakeServiceUser{id: "su-1", name: "deployer", tokenID: "ak-su-1", scoped: true}
	fake.mu.Unlock()
	out, err := invoke(t, s, "getServiceUser", map[string]property.Value{"name": str("deployer")})
	require.NoError(t, err)
	require.Equal(t, "su-1", getStr(t, out, "serviceUserId"))
	require.Equal(t, "ak-su-1", getStr(t, out, "serviceTokenId"))
	require.True(t, out.Get("scoped").AsBool())
	_, hasSecret := out.GetOk("serviceTokenSecret")
	require.False(t, hasSecret, "the lookup never returns a secret")
	_, err = invoke(t, s, "getServiceUser", map[string]property.Value{"name": str("nope")})
	require.ErrorContains(t, err, `service user "nope"`)
}

// ---------------------------------------------------------------------------
// Proxy
// ---------------------------------------------------------------------------

func TestProxy_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Proxy", "egress")

	inputs := pm(map[string]property.Value{"name": str("egress"), "region": str("us-east"), "ipCount": num(2)})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, created.ID, getStr(t, state, "proxyId"))
	require.Equal(t, "main", getStr(t, state, "environment"))
	require.Len(t, strList(state, "ips"), 2)

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// ipCount is an in-place change: grow, then shrink.
	three := pm(map[string]property.Value{"name": str("egress"), "region": str("us-east"), "ipCount": num(3)})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: three, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["ipCount"].Kind)
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: three, OldInputs: inputs})
	require.NoError(t, err)
	state = upd.Properties
	require.Len(t, strList(state, "ips"), 3)
	fake.mu.Lock()
	require.Len(t, fake.proxies[created.ID].ips, 3)
	fake.mu.Unlock()

	one := pm(map[string]property.Value{"name": str("egress"), "region": str("us-east"), "ipCount": num(1)})
	upd, err = s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: one, OldInputs: three})
	require.NoError(t, err)
	state = upd.Properties
	require.Len(t, strList(state, "ips"), 1)
	fake.mu.Lock()
	require.Len(t, fake.proxies[created.ID].ips, 1)
	fake.mu.Unlock()

	// Name, region and environment changes replace.
	for field, next := range map[string]map[string]property.Value{
		"name":        {"name": str("egress2"), "region": str("us-east"), "ipCount": num(1)},
		"region":      {"name": str("egress"), "region": str("eu-west"), "ipCount": num(1)},
		"environment": {"name": str("egress"), "region": str("us-east"), "ipCount": num(1), "environment": str("staging")},
	} {
		d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: pm(next), OldInputs: one})
		require.NoError(t, err)
		require.Equal(t, pgp.UpdateReplace, d.DetailedDiff[field].Kind, field)
	}

	// Drift: an address added outside Pulumi shows up in ipCount.
	fake.mu.Lock()
	fake.proxies[created.ID].ips = append(fake.proxies[created.ID].ips, "198.51.100.9")
	fake.mu.Unlock()
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: one})
	require.NoError(t, err)
	require.Len(t, strList(read.Properties, "ips"), 2)
	require.Equal(t, float64(2), read.Inputs.Get("ipCount").AsNumber())
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: one, OldInputs: read.Inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["ipCount"].Kind, "the program's ipCount reconciles the drift")

	// Import by ID alone: name, environment and region come from Modal.
	imp, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "egress", getStr(t, imp.Inputs, "name"))
	require.Equal(t, "main", getStr(t, imp.Inputs, "environment"))
	require.Equal(t, "us-east", getStr(t, imp.Inputs, "region"))
	_, hasCount := imp.Inputs.GetOk("ipCount")
	require.False(t, hasCount, "an unmanaged ipCount stays unset")

	// The lookup sees the resource.
	out, err := invoke(t, s, "getProxy", map[string]property.Value{"name": str("egress")})
	require.NoError(t, err)
	require.Equal(t, created.ID, getStr(t, out, "proxyId"))

	// Deleted outside Pulumi, then delete again is a no-op.
	fake.mu.Lock()
	delete(fake.proxies, created.ID)
	fake.mu.Unlock()
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: one})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
}

func TestProxy_DefaultsDeleteAndErrors(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Proxy", "p")

	// No region or ipCount: Modal's defaults are recorded and left alone.
	in := pm(map[string]property.Value{"name": str("plain")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.NoError(t, err)
	require.Equal(t, "us-east", getStr(t, created.Properties, "region"))
	require.Len(t, strList(created.Properties, "ips"), 1)
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: in, OldInputs: in})
	require.NoError(t, err)
	requireNoChanges(t, d)

	_, err = s.Create(pgp.CreateRequest{Urn: modalUrn("Proxy", "dup"), Properties: in})
	require.ErrorContains(t, err, `create proxy "plain"`)
	_, err = s.Create(pgp.CreateRequest{Urn: modalUrn("Proxy", "zero"), Properties: pm(map[string]property.Value{
		"name": str("zero"), "ipCount": num(0),
	})})
	require.ErrorContains(t, err, "ipCount must be at least 1")

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: created.Properties}))
	fake.mu.Lock()
	require.Empty(t, fake.proxies)
	fake.mu.Unlock()
}

// ---------------------------------------------------------------------------
// AppDeployment
// ---------------------------------------------------------------------------

// fakeCLI writes a stand-in for the Modal CLI that records what it was run
// with, and returns its path and the log it appends to. FAKE_FAIL makes it
// fail after echoing the credentials it received, to prove they are redacted.
func fakeCLI(t *testing.T) (script, logFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a shell script")
	}
	dir := t.TempDir()
	script = filepath.Join(dir, "modal")
	logFile = filepath.Join(dir, "calls.log")
	body := "#!/bin/sh\n" +
		"{\n" +
		"  echo \"ARGS: $*\"\n" +
		"  echo \"PWD: $(pwd)\"\n" +
		"  echo \"MODAL_TOKEN_ID=$MODAL_TOKEN_ID\"\n" +
		"  echo \"MODAL_ENVIRONMENT=$MODAL_ENVIRONMENT\"\n" +
		"  echo \"MODAL_SERVER_URL=$MODAL_SERVER_URL\"\n" +
		"  echo \"EXTRA=$EXTRA\"\n" +
		"} >> \"" + logFile + "\"\n" +
		"if [ -n \"$FAKE_FAIL\" ]; then\n" +
		"  echo \"boom: token $MODAL_TOKEN_SECRET and extra $EXTRA\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	return script, logFile
}

func writeApp(t *testing.T, content string) (dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "app.py")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	return dir, file
}

func deployInputs(file, script string, extra map[string]property.Value) property.Map {
	m := map[string]property.Value{
		"name":         str("my-app"),
		"appFile":      str(file),
		"modalCommand": strArray(script),
	}
	for k, v := range extra {
		m[k] = v
	}
	return pm(m)
}

func TestDeployment_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	script, logFile := fakeCLI(t)
	dir, file := writeApp(t, "import modal\napp = modal.App('my-app')\n")
	urn := modalUrn("AppDeployment", "d")

	// The CLI creates the App; the fake CLI cannot, so seed it as it would.
	fake.mu.Lock()
	fake.apps["ap-dep"] = &fakeApp{id: "ap-dep", name: "my-app", env: "main", tags: map[string]string{},
		functions: map[string]string{"web": "https://acme--my-app-web.modal.run", "cron": ""}}
	fake.mu.Unlock()

	inputs := deployInputs(file, script, map[string]property.Value{
		"tag": str("v1"),
		"env": strMap(map[string]string{"EXTRA": "hello-extra"}),
	})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Equal(t, "ap-dep", created.ID)
	require.Equal(t, "ap-dep", getStr(t, state, "appId"))
	require.Equal(t, "main", getStr(t, state, "environment"))
	require.NotEmpty(t, getStr(t, state, "sourceHash"))
	require.Equal(t, []string{"cron", "web"}, strList(state, "functions"))
	require.Equal(t, "https://acme--my-app-web.modal.run", state.Get("webUrls").AsMap().Get("web").AsString())
	_, cronHasURL := state.Get("webUrls").AsMap().GetOk("cron")
	require.False(t, cronHasURL, "a Function without a web endpoint has no URL")

	// The CLI got the right command line, directory, credentials and extras.
	logged, err := os.ReadFile(logFile)
	require.NoError(t, err)
	log := string(logged)
	require.Contains(t, log, "ARGS: deploy --name my-app --env main --tag v1 "+file)
	resolvedDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	require.Contains(t, log, "PWD: "+resolvedDir)
	require.Contains(t, log, "MODAL_TOKEN_ID=ak-test")
	require.Contains(t, log, "MODAL_ENVIRONMENT=main")
	require.Contains(t, log, "MODAL_SERVER_URL=http://"+addr)
	require.Contains(t, log, "EXTRA=hello-extra")
	require.NotContains(t, log, "as-test", "the token secret is never printed by this test's CLI, and never logged by the provider")

	// Idempotent: same inputs and source mean no diff.
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Editing the source redeploys in place.
	require.NoError(t, os.WriteFile(file, []byte("import modal\napp = modal.App('my-app')\n# v2\n"), 0o644))
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["sourceHash"].Kind)
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	require.NotEqual(t, getStr(t, state, "sourceHash"), getStr(t, upd.Properties, "sourceHash"))
	state = upd.Properties
	logged, err = os.ReadFile(logFile)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(logged), "ARGS: deploy"), "the update runs the CLI again")
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Triggers, tag and the other settings are in-place; name and environment replace.
	for field, extra := range map[string]map[string]property.Value{
		"triggers": {"tag": str("v1"), "env": strMap(map[string]string{"EXTRA": "hello-extra"}), "triggers": strMap(map[string]string{"dep": "1.2"})},
		"tag":      {"tag": str("v2"), "env": strMap(map[string]string{"EXTRA": "hello-extra"})},
		"env":      {"tag": str("v1"), "env": strMap(map[string]string{"EXTRA": "other"})},
	} {
		d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: deployInputs(file, script, extra), OldInputs: inputs})
		require.NoError(t, err)
		require.Equal(t, pgp.Update, d.DetailedDiff[field].Kind, field)
	}
	renamed := deployInputs(file, script, map[string]property.Value{"tag": str("v1"), "env": strMap(map[string]string{"EXTRA": "hello-extra"}), "name": str("other-app")})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: renamed, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["name"].Kind)
	moved := deployInputs(file, script, map[string]property.Value{"tag": str("v1"), "env": strMap(map[string]string{"EXTRA": "hello-extra"}), "environment": str("staging")})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: moved, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["environment"].Kind)

	// Refresh picks up new Functions and URLs.
	fake.mu.Lock()
	fake.apps["ap-dep"].functions["api"] = "https://acme--my-app-api.modal.run"
	fake.mu.Unlock()
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, []string{"api", "cron", "web"}, strList(read.Properties, "functions"))
	require.Equal(t, "https://acme--my-app-api.modal.run", read.Properties.Get("webUrls").AsMap().Get("api").AsString())
	require.Equal(t, file, getStr(t, read.Inputs, "appFile"), "refresh keeps the program's appFile")

	// getAppDeployment sees the same.
	out, err := invoke(t, s, "getAppDeployment", map[string]property.Value{"name": str("my-app")})
	require.NoError(t, err)
	require.Equal(t, "ap-dep", getStr(t, out, "appId"))
	require.Equal(t, "https://acme--my-app-web.modal.run", out.Get("webUrls").AsMap().Get("web").AsString())
	_, err = invoke(t, s, "getAppDeployment", map[string]property.Value{"name": str("missing")})
	require.ErrorContains(t, err, `deployment "missing"`)

	// Delete stops the App; a stopped App reads as gone.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	fake.mu.Lock()
	require.True(t, fake.apps["ap-dep"].stopped)
	fake.mu.Unlock()
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

func TestDeployment_ImportAndStoppedOutsidePulumi(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	script, _ := fakeCLI(t)
	_, file := writeApp(t, "app = 1\n")
	urn := modalUrn("AppDeployment", "d")
	fake.mu.Lock()
	fake.apps["ap-imp"] = &fakeApp{id: "ap-imp", name: "legacy", env: "main", tags: map[string]string{},
		functions: map[string]string{"web": "https://legacy.modal.run"}}
	fake.mu.Unlock()

	imp, err := s.Read(pgp.ReadRequest{ID: "ap-imp", Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "legacy", getStr(t, imp.Inputs, "name"))
	require.Equal(t, "main", getStr(t, imp.Inputs, "environment"))
	require.Equal(t, "https://legacy.modal.run", imp.Properties.Get("webUrls").AsMap().Get("web").AsString())
	require.Empty(t, getStr(t, imp.Properties, "sourceHash"))

	// The program's appFile is a change against the import, so the first
	// update redeploys.
	prog := deployInputs(file, script, map[string]property.Value{"name": str("legacy")})
	d, err := s.Diff(pgp.DiffRequest{ID: "ap-imp", Urn: urn, State: imp.Properties, Inputs: prog, OldInputs: imp.Inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["appFile"].Kind)
	require.Equal(t, pgp.Update, d.DetailedDiff["sourceHash"].Kind)

	// Stopped outside Pulumi: gone.
	fake.mu.Lock()
	fake.apps["ap-imp"].stopped = true
	fake.mu.Unlock()
	gone, err := s.Read(pgp.ReadRequest{ID: "ap-imp", Urn: urn, Properties: imp.Properties, Inputs: imp.Inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
	// Deleting an already-stopped or unknown App is not an error.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: "ap-imp", Urn: urn, Properties: imp.Properties}))
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: "ap-nope", Urn: urn, Properties: imp.Properties}))
}

func TestDeployment_FailuresAreClearAndRedacted(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	script, _ := fakeCLI(t)
	_, file := writeApp(t, "app = 1\n")
	urn := modalUrn("AppDeployment", "d")

	// A failing CLI: the error carries its output but never the credentials.
	failing := deployInputs(file, script, map[string]property.Value{
		"env": strMap(map[string]string{"FAKE_FAIL": "1", "EXTRA": "super-private-value"}),
	})
	_, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: failing})
	require.ErrorContains(t, err, "`modal deploy` failed")
	require.ErrorContains(t, err, "boom: token [redacted]")
	require.NotContains(t, err.Error(), "as-test")
	require.NotContains(t, err.Error(), "super-private-value")

	// The CLI succeeds but never created the App.
	ok := deployInputs(file, script, nil)
	neverCreated, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: ok})
	require.ErrorContains(t, err, "resource failed to initialize")
	require.NotEmpty(t, neverCreated.ID, "the record survives so a later refresh or destroy can find the App by name")

	// Missing CLI and missing app file.
	missingCLI := pm(map[string]property.Value{"name": str("my-app"), "appFile": str(file), "modalCommand": strArray("definitely-not-a-modal-cli")})
	_, err = s.Create(pgp.CreateRequest{Urn: urn, Properties: missingCLI})
	require.ErrorContains(t, err, "was not found on PATH")
	_, err = s.Create(pgp.CreateRequest{Urn: urn, Properties: deployInputs(filepath.Join(t.TempDir(), "nope.py"), script, nil)})
	require.ErrorContains(t, err, "appFile")

	fake.mu.Lock()
	require.Empty(t, fake.apps, "a failed deployment leaves nothing in state")
	fake.mu.Unlock()
}

func TestSourceFingerprint(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	write("app.py", "a")
	write("pkg/util.py", "b")
	base, err := sourceFingerprint(dir)
	require.NoError(t, err)
	require.NotEmpty(t, base)

	// Caches and VCS state never change it.
	write("__pycache__/app.cpython.pyc", "x")
	write(".git/HEAD", "ref")
	write("node_modules/x/index.js", "y")
	same, err := sourceFingerprint(dir)
	require.NoError(t, err)
	require.Equal(t, base, same)

	// Content and file names do.
	write("pkg/util.py", "changed")
	changed, err := sourceFingerprint(dir)
	require.NoError(t, err)
	require.NotEqual(t, base, changed)
	write("pkg/new.py", "")
	added, err := sourceFingerprint(dir)
	require.NoError(t, err)
	require.NotEqual(t, changed, added)

	// A missing directory is quiet (Create reports the real error).
	none, err := sourceFingerprint(filepath.Join(dir, "absent"))
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestCLIEnv(t *testing.T) {
	env := cliEnv([]string{"PATH=/bin", "MODAL_TOKEN_ID=old", "MODAL_PROFILE=x", "HOME=/h"}, "ak", "as", "dev", "api.modal.com:443", map[string]string{"B": "2", "A": "1"})
	require.Contains(t, env, "PATH=/bin")
	require.Contains(t, env, "HOME=/h")
	require.Contains(t, env, "MODAL_TOKEN_ID=ak")
	require.Contains(t, env, "MODAL_TOKEN_SECRET=as")
	require.Contains(t, env, "MODAL_ENVIRONMENT=dev")
	require.Contains(t, env, "MODAL_SERVER_URL=https://api.modal.com:443")
	require.NotContains(t, env, "MODAL_TOKEN_ID=old")
	require.NotContains(t, env, "MODAL_PROFILE=x")
	require.Equal(t, "A=1", env[len(env)-2])
	require.Equal(t, "http://127.0.0.1:9", cliServerURL("127.0.0.1:9"))
}

// The schema marks the write-once token and the CLI environment as secrets, so
// the engine encrypts them in state and masks them in output.
func TestPhase2Schema_SecretsAndNoVersionInput(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	resp, err := s.GetSchema(pgp.GetSchemaRequest{})
	require.NoError(t, err)
	var schema struct {
		Resources map[string]struct {
			Properties      map[string]struct{ Secret bool } `json:"properties"`
			InputProperties map[string]struct{ Secret bool } `json:"inputProperties"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &schema))
	su := schema.Resources["modal:index:ServiceUser"]
	require.True(t, su.Properties["serviceTokenSecret"].Secret)
	dep := schema.Resources["modal:index:AppDeployment"]
	require.True(t, dep.InputProperties["env"].Secret)
	require.True(t, schema.Resources["modal:index:WebhookToken"].Properties["webhookTokenSecret"].Secret)
	for _, name := range []string{"ServiceUser", "Proxy", "AppDeployment", "WebhookToken"} {
		_, hasVersion := schema.Resources["modal:index:"+name].InputProperties["version"]
		require.False(t, hasVersion, name)
	}
}

// If the read-back fails after `modal deploy` succeeded the App is live, so
// Create and Update must keep it in state (ID and partial state) instead of
// returning a bare error, or destroy could never stop it.
func TestAppDeployment_PostDeployFailureKeepsLiveAppInState(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	script, _ := fakeCLI(t)
	_, file := writeApp(t, "app = 1\n")
	urn := modalUrn("AppDeployment", "d")
	fake.mu.Lock()
	fake.apps["ap-live"] = &fakeApp{id: "ap-live", name: "my-app", env: "main", tags: map[string]string{},
		functions: map[string]string{"web": "https://live.modal.run"}}
	fake.failAppLookup = true
	fake.mu.Unlock()

	inputs := deployInputs(file, script, nil)
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.ErrorContains(t, err, "resource failed to initialize")
	require.NotEmpty(t, created.ID, "the live App must be tracked, not leaked")
	require.NotNil(t, created.PartialState)
	require.Equal(t, "my-app", getStr(t, created.Properties, "name"))
	require.NotEmpty(t, getStr(t, created.Properties, "sourceHash"))

	// Once Modal answers again, refresh recovers the App from the recorded name.
	fake.mu.Lock()
	fake.failAppLookup = false
	fake.mu.Unlock()
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "ap-live", read.ID)
	require.Equal(t, "ap-live", getStr(t, read.Properties, "appId"))
	require.Equal(t, "https://live.modal.run", read.Properties.Get("webUrls").AsMap().Get("web").AsString())

	// Destroy of the half-created resource stops the live App.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: created.Properties}))
	fake.mu.Lock()
	require.True(t, fake.apps["ap-live"].stopped, "destroy must stop the App deployed before the read-back failed")
	fake.mu.Unlock()
}

func TestAppDeployment_UpdateReadBackFailureKeepsState(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	script, _ := fakeCLI(t)
	_, file := writeApp(t, "app = 1\n")
	urn := modalUrn("AppDeployment", "d")
	fake.mu.Lock()
	fake.apps["ap-live"] = &fakeApp{id: "ap-live", name: "my-app", env: "main", tags: map[string]string{},
		functions: map[string]string{"web": "https://live.modal.run"}}
	fake.mu.Unlock()
	inputs := deployInputs(file, script, nil)
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)

	// The layout call fails after the redeploy: the update keeps the ID and state.
	fake.mu.Lock()
	fake.failLayout = true
	fake.mu.Unlock()
	next := deployInputs(file, script, map[string]property.Value{"tag": str("v2")})
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: next, OldInputs: inputs})
	require.ErrorContains(t, err, "resource failed to initialize")
	require.NotNil(t, upd.PartialState)
	require.Equal(t, "ap-live", getStr(t, upd.Properties, "appId"))
	require.Equal(t, "v2", getStr(t, upd.Properties, "tag"))

	// A deploy that exits 0 but never created the App still keeps a record.
	fake.mu.Lock()
	fake.failLayout = false
	fake.mu.Unlock()
	never, err := s.Create(pgp.CreateRequest{Urn: modalUrn("AppDeployment", "other"), Properties: pm(map[string]property.Value{
		"name": str("never-created"), "appFile": str(file), "modalCommand": strArray(script),
	})})
	require.ErrorContains(t, err, "resource failed to initialize")
	require.Equal(t, "never-created", never.ID)
}

// An App whose own name starts with "ap-" must still be resolved by name when
// the deploy's read-back failed and the resource ID is the name.
func TestAppDeployment_ApPrefixedNameResolvesAfterFailedReadBack(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	script, _ := fakeCLI(t)
	_, file := writeApp(t, "app = 1\n")
	urn := modalUrn("AppDeployment", "d")
	fake.mu.Lock()
	fake.apps["ap-real1"] = &fakeApp{id: "ap-real1", name: "ap-weird", env: "main", tags: map[string]string{},
		functions: map[string]string{"web": "https://weird.modal.run"}}
	fake.failAppLookup = true
	fake.mu.Unlock()

	inputs := deployInputs(file, script, map[string]property.Value{"name": str("ap-weird")})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.ErrorContains(t, err, "resource failed to initialize")
	require.Equal(t, "ap-weird", created.ID, "the name stands in for the unknown ID")

	fake.mu.Lock()
	fake.failAppLookup = false
	fake.mu.Unlock()
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "ap-real1", read.ID)

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: created.Properties}))
	fake.mu.Lock()
	require.True(t, fake.apps["ap-real1"].stopped)
	fake.mu.Unlock()
}
