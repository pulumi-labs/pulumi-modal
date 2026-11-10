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
	"fmt"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ---------------------------------------------------------------------------
// WorkspaceSettings resource
// ---------------------------------------------------------------------------

// WorkspaceSettings manages the settings of the workspace the API token
// belongs to. A workspace has exactly one, so it is a singleton.
type WorkspaceSettings struct{}

// WorkspaceSettingsArgs are the user-supplied inputs.
type WorkspaceSettingsArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// DefaultEnvironment is the name of the environment used when a client
	// does not name one.
	DefaultEnvironment *string `pulumi:"defaultEnvironment,optional"`
	// ImageBuilderVersion is the image builder version the workspace builds
	// images with.
	ImageBuilderVersion *string `pulumi:"imageBuilderVersion,optional"`
}

// WorkspaceSettingsState is the persisted state.
type WorkspaceSettingsState struct {
	WorkspaceSettingsArgs

	// WorkspaceId is the identifier of the workspace the settings belong to.
	WorkspaceId string `pulumi:"workspaceId"`
}

func (r *WorkspaceSettings) Annotate(a infer.Annotator) {
	a.Describe(&r, "The settings of the Modal workspace the API token belongs to: its default environment and image builder version. "+
		"A workspace has exactly one set of settings, so this is a singleton: declare it at most once per workspace. It is created by "+
		"applying the settings you set and the import ID is the workspace ID. Modal cannot unset either setting, so removing a setting "+
		"from the program, or destroying this resource, leaves the workspace as it is.")
}

func (a *WorkspaceSettingsArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.DefaultEnvironment, "Name of the workspace's default environment, used when a client does not name one. The environment must exist. "+
		"Unset leaves the workspace default as it is.")
	ann.Describe(&a.ImageBuilderVersion, "Image builder version the workspace builds images with, for example `2024.10`. "+
		"Unset leaves the workspace version as it is.")
}

func (s *WorkspaceSettingsState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.WorkspaceId, "The identifier of the workspace the settings belong to. This is also the resource ID.")
}

var _ infer.CustomResource[WorkspaceSettingsArgs, WorkspaceSettingsState] = (*WorkspaceSettings)(nil)
var _ infer.CustomUpdate[WorkspaceSettingsArgs, WorkspaceSettingsState] = (*WorkspaceSettings)(nil)
var _ infer.CustomDiff[WorkspaceSettingsArgs, WorkspaceSettingsState] = (*WorkspaceSettings)(nil)
var _ infer.CustomRead[WorkspaceSettingsArgs, WorkspaceSettingsState] = (*WorkspaceSettings)(nil)
var _ infer.CustomDelete[WorkspaceSettingsState] = (*WorkspaceSettings)(nil)

// Check rejects empty strings, which Modal would refuse anyway, and requires at
// least one setting (a known one: a computed value counts as set).
func (r *WorkspaceSettings) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[WorkspaceSettingsArgs], error) {
	args, failures, err := infer.DefaultCheck[WorkspaceSettingsArgs](ctx, req.NewInputs)
	if err != nil || len(failures) > 0 {
		return infer.CheckResponse[WorkspaceSettingsArgs]{Inputs: args, Failures: failures}, err
	}
	present := 0
	for name, v := range map[string]*string{"defaultEnvironment": args.DefaultEnvironment, "imageBuilderVersion": args.ImageBuilderVersion} {
		raw, ok := req.NewInputs.GetOk(name)
		if ok {
			present++
		}
		if ok && !raw.HasComputed() && v != nil && *v == "" {
			failures = append(failures, pgp.CheckFailure{Property: name, Reason: name + " must not be empty; leave it unset to keep the workspace value"})
		}
	}
	if present == 0 {
		failures = append(failures, pgp.CheckFailure{Property: "defaultEnvironment", Reason: "set at least one of defaultEnvironment and imageBuilderVersion"})
	}
	return infer.CheckResponse[WorkspaceSettingsArgs]{Inputs: args, Failures: failures}, nil
}

// Diff compares only the settings the program sets: an unset one leaves the
// workspace value alone, so it is never a change.
func (r *WorkspaceSettings) Diff(_ context.Context, req infer.DiffRequest[WorkspaceSettingsArgs, WorkspaceSettingsState]) (infer.DiffResponse, error) {
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	if req.Inputs.DefaultEnvironment != nil && derefStr(req.Inputs.DefaultEnvironment) != derefStr(req.State.DefaultEnvironment) {
		resp.DetailedDiff["defaultEnvironment"] = pgp.PropertyDiff{Kind: pgp.Update}
	}
	if req.Inputs.ImageBuilderVersion != nil && derefStr(req.Inputs.ImageBuilderVersion) != derefStr(req.State.ImageBuilderVersion) {
		resp.DetailedDiff["imageBuilderVersion"] = pgp.PropertyDiff{Kind: pgp.Update}
	}
	resp.HasChanges = len(resp.DetailedDiff) > 0
	return resp, nil
}

// applyWorkspaceSettings sets the requested settings that differ from old (a
// nil old sets everything requested), the default environment first. A failure
// of the second call leaves the first applied; the caller does not advance
// state and a retry repeats only what is still different.
func applyWorkspaceSettings(ctx context.Context, client *modalClient, want WorkspaceSettingsArgs, old *WorkspaceSettingsArgs) error {
	if want.DefaultEnvironment != nil && (old == nil || derefStr(old.DefaultEnvironment) != *want.DefaultEnvironment) {
		if _, err := client.stub.WorkspaceSetDefaultEnvironment(ctx, pb.WorkspaceSetDefaultEnvironmentRequest_builder{EnvironmentName: *want.DefaultEnvironment}.Build()); err != nil {
			return fmt.Errorf("set default environment %q: %w", *want.DefaultEnvironment, err)
		}
	}
	if want.ImageBuilderVersion != nil && (old == nil || derefStr(old.ImageBuilderVersion) != *want.ImageBuilderVersion) {
		if _, err := client.stub.WorkspaceSetImageBuilderVersion(ctx, pb.WorkspaceSetImageBuilderVersionRequest_builder{NewImageBuilderVersion: *want.ImageBuilderVersion}.Build()); err != nil {
			return fmt.Errorf("set image builder version %q: %w", *want.ImageBuilderVersion, err)
		}
	}
	return nil
}

func (r *WorkspaceSettings) Create(ctx context.Context, req infer.CreateRequest[WorkspaceSettingsArgs]) (infer.CreateResponse[WorkspaceSettingsState], error) {
	args := req.Inputs
	if req.DryRun {
		return infer.CreateResponse[WorkspaceSettingsState]{Output: WorkspaceSettingsState{WorkspaceSettingsArgs: args}}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[WorkspaceSettingsState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()
	rpcCtx, cancel := callCtx()
	defer cancel()
	info, err := client.stub.TokenInfoGet(rpcCtx, pb.TokenInfoGetRequest_builder{}.Build())
	if err != nil {
		return infer.CreateResponse[WorkspaceSettingsState]{}, fmt.Errorf("modal: look up workspace: %w", err)
	}
	if err := applyWorkspaceSettings(rpcCtx, client, args, nil); err != nil {
		return infer.CreateResponse[WorkspaceSettingsState]{}, fmt.Errorf("modal: %w", err)
	}
	id := info.GetWorkspaceId()
	return infer.CreateResponse[WorkspaceSettingsState]{ID: id, Output: WorkspaceSettingsState{WorkspaceSettingsArgs: args, WorkspaceId: id}}, nil
}

func (r *WorkspaceSettings) Update(ctx context.Context, req infer.UpdateRequest[WorkspaceSettingsArgs, WorkspaceSettingsState]) (infer.UpdateResponse[WorkspaceSettingsState], error) {
	args := req.Inputs
	state := req.State
	if args.DefaultEnvironment != nil {
		state.DefaultEnvironment = args.DefaultEnvironment
	}
	if args.ImageBuilderVersion != nil {
		state.ImageBuilderVersion = args.ImageBuilderVersion
	}
	state.TokenID, state.TokenSecret = args.TokenID, args.TokenSecret
	if req.DryRun {
		return infer.UpdateResponse[WorkspaceSettingsState]{Output: state}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[WorkspaceSettingsState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()
	rpcCtx, cancel := callCtx()
	defer cancel()
	old := req.State.WorkspaceSettingsArgs
	if err := applyWorkspaceSettings(rpcCtx, client, args, &old); err != nil {
		return infer.UpdateResponse[WorkspaceSettingsState]{}, fmt.Errorf("modal: %w", err)
	}
	return infer.UpdateResponse[WorkspaceSettingsState]{Output: state}, nil
}

// Read refreshes the settings. The ID is the workspace ID, so a token that
// belongs to another workspace cannot see it: that is reported as missing.
// Only the settings the program manages are refreshed (all of them on import).
func (r *WorkspaceSettings) Read(ctx context.Context, req infer.ReadRequest[WorkspaceSettingsArgs, WorkspaceSettingsState]) (infer.ReadResponse[WorkspaceSettingsArgs, WorkspaceSettingsState], error) {
	empty := infer.ReadResponse[WorkspaceSettingsArgs, WorkspaceSettingsState]{}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()
	rpcCtx, cancel := callCtx()
	defer cancel()
	info, err := client.stub.TokenInfoGet(rpcCtx, pb.TokenInfoGetRequest_builder{}.Build())
	if err != nil {
		return empty, fmt.Errorf("modal: look up workspace: %w", err)
	}
	if info.GetWorkspaceId() != req.ID {
		if err := notFoundOnImport("workspace settings", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}
	got, err := client.stub.WorkspaceSettings(rpcCtx, &emptypb.Empty{})
	if err != nil {
		return empty, fmt.Errorf("modal: read workspace settings: %w", err)
	}
	importing := req.State.WorkspaceId == "" && req.Inputs.DefaultEnvironment == nil && req.Inputs.ImageBuilderVersion == nil
	state := req.State
	state.WorkspaceId = req.ID
	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	if importing || state.DefaultEnvironment != nil {
		v := got.GetDefaultEnvironmentName()
		state.DefaultEnvironment = &v
		inputs.DefaultEnvironment = &v
	}
	if importing || state.ImageBuilderVersion != nil {
		v := got.GetImageBuilderVersion()
		state.ImageBuilderVersion = &v
		inputs.ImageBuilderVersion = &v
	}
	return infer.ReadResponse[WorkspaceSettingsArgs, WorkspaceSettingsState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

// Delete only forgets the settings: Modal has no call that unsets them.
func (r *WorkspaceSettings) Delete(context.Context, infer.DeleteRequest[WorkspaceSettingsState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, nil
}

// ---------------------------------------------------------------------------
// getWorkspaceSettings function
// ---------------------------------------------------------------------------

// GetWorkspaceSettings reads the settings of the workspace the token belongs to.
type GetWorkspaceSettings struct{}

// GetWorkspaceSettingsArgs has no arguments: the workspace is the token's.
type GetWorkspaceSettingsArgs struct{}

// GetWorkspaceSettingsResult is the workspace's settings.
type GetWorkspaceSettingsResult struct {
	WorkspaceId         string `pulumi:"workspaceId"`
	DefaultEnvironment  string `pulumi:"defaultEnvironment"`
	ImageBuilderVersion string `pulumi:"imageBuilderVersion"`
}

func (r *GetWorkspaceSettingsResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.WorkspaceId, "The identifier of the workspace the token belongs to.")
	ann.Describe(&r.DefaultEnvironment, "Name of the workspace's default environment.")
	ann.Describe(&r.ImageBuilderVersion, "The image builder version the workspace builds images with.")
}

func (f *GetWorkspaceSettings) Annotate(a infer.Annotator) {
	a.Describe(&f, "Read the settings of the Modal workspace the provider's token belongs to: its default environment and image builder version.")
}

func (f *GetWorkspaceSettings) Invoke(ctx context.Context, _ infer.FunctionRequest[GetWorkspaceSettingsArgs]) (infer.FunctionResponse[GetWorkspaceSettingsResult], error) {
	out := infer.FunctionResponse[GetWorkspaceSettingsResult]{}
	client, _, closeFn, err := lookupSession(ctx, nil)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	info, err := client.stub.TokenInfoGet(rpcCtx, pb.TokenInfoGetRequest_builder{}.Build())
	if err != nil {
		return out, fmt.Errorf("modal: look up workspace: %w", err)
	}
	got, err := client.stub.WorkspaceSettings(rpcCtx, &emptypb.Empty{})
	if err != nil {
		return out, fmt.Errorf("modal: read workspace settings: %w", err)
	}
	out.Output = GetWorkspaceSettingsResult{
		WorkspaceId: info.GetWorkspaceId(), DefaultEnvironment: got.GetDefaultEnvironmentName(), ImageBuilderVersion: got.GetImageBuilderVersion(),
	}
	return out, nil
}
