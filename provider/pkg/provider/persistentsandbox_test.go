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
	"context"
	"testing"
	"time"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakePersistentSandbox is one persistent Sandbox held by the fake control
// plane. Like the live API, Get returns identifiers only, never the definition.
type fakePersistentSandbox struct {
	id         string
	sessionID  string
	appID      string
	def        *pb.Sandbox
	terminated bool
}

func (f *fakeModal) PersistentSandboxCreate(_ context.Context, req *pb.PersistentSandboxCreateRequest) (*pb.PersistentSandboxCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.GetAppId() == "" || req.GetDefinition().GetImageId() == "" {
		return nil, status.Error(codes.InvalidArgument, "app and image are required")
	}
	id := f.nextID("sp")
	sess := f.nextID("sb")
	f.persistent[id] = &fakePersistentSandbox{id: id, sessionID: sess, appID: req.GetAppId(), def: req.GetDefinition()}
	return pb.PersistentSandboxCreateResponse_builder{PersistentSandboxId: id, SandboxId: sess}.Build(), nil
}

func (f *fakeModal) PersistentSandboxGet(_ context.Context, req *pb.PersistentSandboxGetRequest) (*pb.PersistentSandboxGetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp, ok := f.persistent[req.GetPersistentSandboxId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "not found")
	}
	b := pb.PersistentSandboxGetResponse_builder{PersistentSandboxId: sp.id, SandboxId: sp.sessionID}
	if sp.terminated {
		b.TerminatedAt = timestamppb.New(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		b.SandboxId = ""
	}
	return b.Build(), nil
}

func (f *fakeModal) PersistentSandboxTerminate(_ context.Context, req *pb.PersistentSandboxTerminateRequest) (*pb.PersistentSandboxTerminateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp, ok := f.persistent[req.GetPersistentSandboxId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "not found")
	}
	sp.terminated = true
	return &pb.PersistentSandboxTerminateResponse{}, nil
}

func persistentInputs() map[string]property.Value {
	return map[string]property.Value{
		"appId":        str("ap-1"),
		"imageId":      str("im-1"),
		"command":      strArray("sleep", "infinity"),
		"workdir":      str("/work"),
		"cpu":          property.New(0.5),
		"memoryMiB":    num(1024),
		"regions":      strArray("us-east", "us-west"),
		"blockNetwork": property.New(true),
		"secretIds":    strArray("st-1", "st-2"),
		"volumes":      strMap(map[string]string{"/data": "vo-1"}),
	}
}

func TestPersistentSandbox_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("PersistentSandbox", "box")
	inputs := pm(persistentInputs())

	// Preview creates nothing and leaves the ID unknown.
	preview, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs, DryRun: true})
	require.NoError(t, err)
	require.Empty(t, preview.ID)
	fake.mu.Lock()
	require.Empty(t, fake.persistent)
	fake.mu.Unlock()

	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.NotEmpty(t, created.ID)
	require.Equal(t, created.ID, getStr(t, state, "persistentSandboxId"))
	require.NotEmpty(t, getStr(t, state, "sandboxId"))

	// The definition sent to Modal carries the inputs and the fixed session limits.
	fake.mu.Lock()
	sp := fake.persistent[created.ID]
	fake.mu.Unlock()
	def := sp.def
	require.Equal(t, "ap-1", sp.appID)
	require.Equal(t, "im-1", def.GetImageId())
	require.Equal(t, []string{"sleep", "infinity"}, def.GetEntrypointArgs())
	require.Equal(t, "/work", def.GetWorkdir())
	require.Equal(t, uint32(500), def.GetResources().GetMilliCpu())
	require.Equal(t, uint32(1024), def.GetResources().GetMemoryMb())
	require.Equal(t, pb.NetworkAccess_BLOCKED, def.GetNetworkAccess().GetNetworkAccessType())
	require.Equal(t, []string{"st-1", "st-2"}, def.GetSecretIds())
	require.Equal(t, []string{"us-east", "us-west"}, def.GetSchedulerPlacement().GetRegions())
	require.Len(t, def.GetVolumeMounts(), 1)
	require.Equal(t, "/data", def.GetVolumeMounts()[0].GetMountPath())
	require.Equal(t, "vo-1", def.GetVolumeMounts()[0].GetVolumeId())
	require.Equal(t, uint32(3600), def.GetTimeoutSecs())
	require.Equal(t, uint32(300), def.GetIdleTimeoutSecs())

	// Same inputs again: no diff.
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Reordered sets and explicit defaults (blockNetwork false, empty lists) are not changes.
	reordered := persistentInputs()
	reordered["regions"] = strArray("us-west", "us-east")
	reordered["secretIds"] = strArray("st-2", "st-1")
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: pm(reordered), OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	minimal := pm(map[string]property.Value{"appId": str("ap-1"), "imageId": str("im-1")})
	min, err := s.Create(pgp.CreateRequest{Urn: modalUrn("PersistentSandbox", "min"), Properties: minimal})
	require.NoError(t, err)
	withDefaults := pm(map[string]property.Value{
		"appId": str("ap-1"), "imageId": str("im-1"), "blockNetwork": property.New(false),
		"command": strArray(), "regions": strArray(), "secretIds": strArray(), "volumes": strMap(nil),
	})
	d, err = s.Diff(pgp.DiffRequest{ID: min.ID, Urn: modalUrn("PersistentSandbox", "min"), State: min.Properties, Inputs: withDefaults, OldInputs: minimal})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Every input replaces: Modal cannot edit the definition.
	changes := map[string]func(m map[string]property.Value){
		"appId":        func(m map[string]property.Value) { m["appId"] = str("ap-2") },
		"imageId":      func(m map[string]property.Value) { m["imageId"] = str("im-2") },
		"command":      func(m map[string]property.Value) { m["command"] = strArray("sleep", "60") },
		"workdir":      func(m map[string]property.Value) { m["workdir"] = str("/other") },
		"cpu":          func(m map[string]property.Value) { m["cpu"] = property.New(2.0) },
		"memoryMiB":    func(m map[string]property.Value) { m["memoryMiB"] = num(2048) },
		"regions":      func(m map[string]property.Value) { m["regions"] = strArray("eu-west") },
		"blockNetwork": func(m map[string]property.Value) { m["blockNetwork"] = property.New(false) },
		"secretIds":    func(m map[string]property.Value) { m["secretIds"] = strArray("st-3") },
		"volumes":      func(m map[string]property.Value) { m["volumes"] = strMap(map[string]string{"/data": "vo-2"}) },
	}
	for field, mutate := range changes {
		next := persistentInputs()
		mutate(next)
		d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: pm(next), OldInputs: inputs})
		require.NoError(t, err)
		require.Equal(t, pgp.UpdateReplace, d.DetailedDiff[field].Kind, field)
	}

	// Refresh keeps the recorded definition and picks up a resumed session.
	fake.mu.Lock()
	fake.persistent[created.ID].sessionID = "sb-resumed"
	fake.mu.Unlock()
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, "sb-resumed", getStr(t, read.Properties, "sandboxId"))
	require.Equal(t, "ap-1", getStr(t, read.Inputs, "appId"))
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Delete terminates, and is idempotent.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: read.Properties}))
	fake.mu.Lock()
	require.True(t, fake.persistent[created.ID].terminated)
	fake.mu.Unlock()
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: read.Properties}))
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: "sp-gone", Urn: urn, Properties: read.Properties}))
}

func TestPersistentSandbox_ImportAndDeletedOutsidePulumi(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("PersistentSandbox", "box")
	inputs := pm(persistentInputs())
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)

	// Import by ID alone: identifiers come back, the definition does not.
	imp, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn})
	require.NoError(t, err)
	require.Equal(t, created.ID, imp.ID)
	require.Equal(t, created.ID, getStr(t, imp.Properties, "persistentSandboxId"))
	require.NotEmpty(t, getStr(t, imp.Properties, "sandboxId"))
	// The program's definition is adopted without replacing the live Sandbox.
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: imp.Properties, Inputs: inputs, OldInputs: imp.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Terminated outside Pulumi: refresh drops it.
	fake.mu.Lock()
	fake.persistent[created.ID].terminated = true
	fake.mu.Unlock()
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
	// Import of a terminated Sandbox fails rather than storing an empty resource.
	_, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn})
	require.ErrorContains(t, err, "does not exist")

	// Deleted outright: same.
	fake.mu.Lock()
	delete(fake.persistent, created.ID)
	fake.mu.Unlock()
	gone, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

func TestPersistentSandbox_CreateErrors(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("PersistentSandbox", "bad")
	for _, tc := range []struct {
		name string
		in   map[string]property.Value
		want string
	}{
		{"cpu", map[string]property.Value{"appId": str("ap-1"), "imageId": str("im-1"), "cpu": property.New(-1.0)}, "cpu"},
		{"memory", map[string]property.Value{"appId": str("ap-1"), "imageId": str("im-1"), "memoryMiB": num(0)}, "memoryMiB"},
	} {
		_, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: pm(tc.in)})
		require.ErrorContains(t, err, tc.want, tc.name)
	}
	_, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: pm(map[string]property.Value{"appId": str(""), "imageId": str("im-1")})})
	require.Error(t, err)
}

func TestGetPersistentSandbox(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	created, err := s.Create(pgp.CreateRequest{Urn: modalUrn("PersistentSandbox", "box"), Properties: pm(persistentInputs())})
	require.NoError(t, err)

	out, err := invoke(t, s, "getPersistentSandbox", map[string]property.Value{"persistentSandboxId": str(created.ID)})
	require.NoError(t, err)
	require.Equal(t, created.ID, getStr(t, out, "persistentSandboxId"))
	require.NotEmpty(t, getStr(t, out, "sandboxId"))
	require.False(t, out.Get("terminated").AsBool())
	require.Empty(t, getStr(t, out, "terminatedAt"))

	fake.mu.Lock()
	fake.persistent[created.ID].terminated = true
	fake.mu.Unlock()
	out, err = invoke(t, s, "getPersistentSandbox", map[string]property.Value{"persistentSandboxId": str(created.ID)})
	require.NoError(t, err)
	require.True(t, out.Get("terminated").AsBool())
	require.Equal(t, "2026-11-17T12:00:00Z", getStr(t, out, "terminatedAt"))

	_, err = invoke(t, s, "getPersistentSandbox", map[string]property.Value{"persistentSandboxId": str("sp-nope")})
	require.ErrorContains(t, err, `persistent sandbox "sp-nope" was not found`)
}

// Invalid definitions must fail Check (preview), not only apply: a replace
// deletes the live Sandbox before the replacement is created.
func TestPersistentSandbox_CheckRejectsInvalidDefinition(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("PersistentSandbox", "chk")
	base := func(extra map[string]property.Value) property.Map {
		m := map[string]property.Value{"appId": str("ap-1"), "imageId": str("im-1")}
		for k, v := range extra {
			m[k] = v
		}
		return pm(m)
	}
	for _, tc := range []struct {
		name  string
		extra map[string]property.Value
		want  string
	}{
		{"cpu", map[string]property.Value{"cpu": property.New(-1.0)}, "cpu"},
		{"memory", map[string]property.Value{"memoryMiB": num(0)}, "memoryMiB"},
		{"workdir", map[string]property.Value{"workdir": str("work")}, "workdir"},
	} {
		resp, err := s.Check(pgp.CheckRequest{Urn: urn, Inputs: base(tc.extra)})
		require.NoError(t, err, tc.name)
		require.Len(t, resp.Failures, 1, tc.name)
		require.Equal(t, tc.want, resp.Failures[0].Property, tc.name)
	}
	ok, err := s.Check(pgp.CheckRequest{Urn: urn, Inputs: base(map[string]property.Value{"workdir": str("/work"), "cpu": property.New(0.5), "memoryMiB": num(512)})})
	require.NoError(t, err)
	require.Empty(t, ok.Failures)
}
