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
)

// ---------------------------------------------------------------------------
// Volume resource
// ---------------------------------------------------------------------------

// Volume provisions a Modal Volume — a persistent, distributed filesystem
// mountable into Modal Functions and Sandboxes for caching model weights,
// datasets, or other large files across runs.
type Volume struct{}

// VolumeArgs are the user-supplied inputs for a Modal Volume.
type VolumeArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the volume's unique name within its environment. Changing it
	// renames the volume in place.
	Name string `pulumi:"name"`

	// Environment is the Modal environment (workspace namespace) the volume
	// lives in. Changing this forces a new resource. Falls back to the
	// provider-level environment.
	Environment *string `pulumi:"environment,optional"`

	// FsVersion selects the volume filesystem version: "v1" (legacy) or "v2"
	// (default, recommended for new volumes). Changing this forces a new
	// resource.
	//
	// The property is deliberately not called "version": pulumi-go-provider
	// silently drops any input property named "version" before Diff runs
	// (https://github.com/pulumi/pulumi-go-provider/issues/704), which made every update look like
	// a change and replaced the volume on every deployment.
	FsVersion *string `pulumi:"fsVersion,optional"`
}

// VolumeState holds the computed outputs for a Modal Volume.
type VolumeState struct {
	VolumeArgs

	// VolumeId is the Modal-assigned unique identifier for the volume
	// (distinct from its human-readable Name).
	VolumeId string `pulumi:"volumeId"`
}

func (r *Volume) Annotate(a infer.Annotator) {
	a.Describe(&r, "A Modal Volume — a persistent, distributed filesystem mountable into Modal "+
		"Functions and Sandboxes for caching model weights, datasets, or other large files across runs. "+
		"See https://modal.com/docs/guide/volumes.")
}

func (a *VolumeArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The volume's unique name within its environment. Changing this renames the volume in place.")
	ann.Describe(&a.Environment, "The Modal environment the volume lives in. Changing this forces a new resource, "+
		"which deletes the volume's data. When unset, the provider-level environment or the account's default "+
		"environment is used.")
	ann.Describe(&a.FsVersion, "Volume filesystem version: \"v1\" or \"v2\". Defaults to \"v2\" (Modal's recommended "+
		"backend; note that Modal's own SDKs default to \"v1\"). The effective version is always reported as an "+
		"output. Changing this forces a new resource and deletes the volume's data: the old volume is deleted "+
		"before the new one is created because both share a name.")
}

var _ infer.CustomResource[VolumeArgs, VolumeState] = (*Volume)(nil)
var _ infer.CustomDiff[VolumeArgs, VolumeState] = (*Volume)(nil)
var _ infer.CustomCheck[VolumeArgs] = (*Volume)(nil)
var _ infer.CustomUpdate[VolumeArgs, VolumeState] = (*Volume)(nil)
var _ infer.CustomRead[VolumeArgs, VolumeState] = (*Volume)(nil)
var _ infer.CustomDelete[VolumeState] = (*Volume)(nil)
var _ infer.CustomStateMigrations[VolumeState] = (*Volume)(nil)

// volumeStateV0 is the state shape written by v0.1.0, where fsVersion was
// called "version".
type volumeStateV0 struct {
	TokenID     *string `pulumi:"tokenId,optional" provider:"secret"`
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`
	Name        string  `pulumi:"name"`
	Environment *string `pulumi:"environment,optional"`
	Version     *string `pulumi:"version,optional"`
	VolumeId    string  `pulumi:"volumeId"`
}

// StateMigrations moves v0.1.0 state (property "version") to fsVersion.
func (r *Volume) StateMigrations(context.Context) []infer.StateMigrationFunc[VolumeState] {
	return []infer.StateMigrationFunc[VolumeState]{
		infer.StateMigration(func(_ context.Context, old volumeStateV0) (infer.MigrationResult[VolumeState], error) {
			return infer.MigrationResult[VolumeState]{Result: &VolumeState{
				VolumeArgs: VolumeArgs{
					TokenID:     old.TokenID,
					TokenSecret: old.TokenSecret,
					Name:        old.Name,
					Environment: old.Environment,
					FsVersion:   old.Version,
				},
				VolumeId: old.VolumeId,
			}}, nil
		}),
	}
}

// parseFsVersion validates an fsVersion input and returns the wire enum. An
// unset value selects v2.
func parseFsVersion(v *string) (pb.VolumeFsVersion, error) {
	if v == nil || *v == "" {
		return pb.VolumeFsVersion_VOLUME_FS_VERSION_V2, nil
	}
	switch *v {
	case "v1":
		return pb.VolumeFsVersion_VOLUME_FS_VERSION_V1, nil
	case "v2":
		return pb.VolumeFsVersion_VOLUME_FS_VERSION_V2, nil
	default:
		return pb.VolumeFsVersion_VOLUME_FS_VERSION_UNSPECIFIED,
			fmt.Errorf("modal: volume fsVersion must be \"v1\" or \"v2\", got %q", *v)
	}
}

// fsVersionString maps the wire enum back to "v1"/"v2". Modal reports an
// unspecified version for legacy volumes, which are v1.
func fsVersionString(v pb.VolumeFsVersion) string {
	if v == pb.VolumeFsVersion_VOLUME_FS_VERSION_V2 {
		return "v2"
	}
	return "v1"
}

// Check validates fsVersion at preview time.
func (r *Volume) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[VolumeArgs], error) {
	args, failures, err := infer.DefaultCheck[VolumeArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[VolumeArgs]{}, err
	}
	if _, verr := parseFsVersion(args.FsVersion); verr != nil {
		failures = append(failures, pgp.CheckFailure{Property: "fsVersion", Reason: verr.Error()})
	}
	return infer.CheckResponse[VolumeArgs]{Inputs: args, Failures: failures}, nil
}

// Diff renames in place when only the name changed, and replaces when the
// environment or filesystem version changes: both are immutable on Modal.
func (r *Volume) Diff(ctx context.Context, req infer.DiffRequest[VolumeArgs, VolumeState]) (infer.DiffResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}

	renamed := req.Inputs.Name != req.State.Name
	if renamed {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.Update}
		resp.HasChanges = true
	}
	envChanged := environmentChanged(req.Inputs.Environment, req.State.Environment, cfg)
	if envChanged {
		resp.DetailedDiff["environment"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	// An unset input means "default", and a state without a recorded version
	// predates fsVersion tracking: neither can differ.
	fsChanged := derefStr(req.Inputs.FsVersion) != "" && derefStr(req.State.FsVersion) != "" &&
		*req.Inputs.FsVersion != *req.State.FsVersion
	if fsChanged {
		resp.DetailedDiff["fsVersion"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	// A replacement that keeps the same (name, environment) pair would collide
	// with the old volume, because creation fails if the name exists.
	resp.DeleteBeforeReplace = fsChanged && !renamed && !envChanged
	return resp, nil
}

func (r *Volume) Create(ctx context.Context, req infer.CreateRequest[VolumeArgs]) (infer.CreateResponse[VolumeState], error) {
	args := req.Inputs
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		state := VolumeState{VolumeArgs: args}
		if env := resolveStr(args.Environment, cfg.Environment); env != "" {
			state.Environment = &env
		}
		return infer.CreateResponse[VolumeState]{ID: args.Name, Output: state}, nil
	}

	version, err := parseFsVersion(args.FsVersion)
	if err != nil {
		return infer.CreateResponse[VolumeState]{}, err
	}
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[VolumeState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	environment := effectiveEnvironment(rpcCtx, client, args.Environment, cfg)
	resp, err := client.stub.VolumeGetOrCreate(rpcCtx, pb.VolumeGetOrCreateRequest_builder{
		DeploymentName:     args.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS,
		Version:            version,
	}.Build())
	if err != nil {
		return infer.CreateResponse[VolumeState]{}, fmt.Errorf("modal: create volume %q: %w", args.Name, err)
	}

	effective := version
	if resp.GetVersion() != pb.VolumeFsVersion_VOLUME_FS_VERSION_UNSPECIFIED {
		effective = resp.GetVersion()
	}
	state := VolumeState{VolumeArgs: args, VolumeId: resp.GetVolumeId()}
	fsv := fsVersionString(effective)
	state.FsVersion = &fsv
	if environment != "" {
		state.Environment = &environment
	}
	return infer.CreateResponse[VolumeState]{ID: resp.GetVolumeId(), Output: state}, nil
}

// Read refreshes the volume by ID, so it works for refresh and for import
// from the ID alone.
func (r *Volume) Read(ctx context.Context, req infer.ReadRequest[VolumeArgs, VolumeState]) (infer.ReadResponse[VolumeArgs, VolumeState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[VolumeArgs, VolumeState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.VolumeGetById(rpcCtx, pb.VolumeGetByIdRequest_builder{VolumeId: req.ID}.Build())
	if isNotFound(err) {
		// Deleted outside Pulumi: signal removal.
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("modal: read volume %q: %w", req.ID, err)
	}
	meta := resp.GetMetadata()

	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	state.VolumeId = req.ID
	state.Name = meta.GetName()
	fsv := fsVersionString(meta.GetVersion())
	state.FsVersion = &fsv

	// Modal does not report a volume's environment, so keep what is recorded.
	environment := derefStr(state.Environment)
	if environment == "" {
		environment = effectiveEnvironment(rpcCtx, client, nil, cfg)
	}
	if importing {
		// Confirm the ID lives in the environment we are about to record.
		lookup, lerr := client.stub.VolumeGetOrCreate(rpcCtx, pb.VolumeGetOrCreateRequest_builder{
			DeploymentName:     meta.GetName(),
			EnvironmentName:    environment,
			ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
		}.Build())
		if lerr != nil || lookup.GetVolumeId() != req.ID {
			return empty, fmt.Errorf("modal: volume %q (%s) was not found in environment %q; "+
				"set the modal:environment provider config (or MODAL_ENVIRONMENT) to the environment the volume lives in and retry the import",
				meta.GetName(), req.ID, environment)
		}
	}
	if environment != "" {
		state.Environment = &environment
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = meta.GetName()
	if importing || inputs.FsVersion != nil {
		inputs.FsVersion = &fsv
	}
	if importing {
		inputs.Environment = &environment
		if environment == "" {
			inputs.Environment = nil
		}
	}
	return infer.ReadResponse[VolumeArgs, VolumeState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

// Update renames the volume in place; every other property change is a replace.
func (r *Volume) Update(ctx context.Context, req infer.UpdateRequest[VolumeArgs, VolumeState]) (infer.UpdateResponse[VolumeState], error) {
	state := req.State
	state.Name = req.Inputs.Name
	state.TokenID, state.TokenSecret = req.Inputs.TokenID, req.Inputs.TokenSecret
	if req.DryRun || req.Inputs.Name == req.State.Name {
		return infer.UpdateResponse[VolumeState]{Output: state}, nil
	}

	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.Inputs.TokenID, req.Inputs.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[VolumeState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	if _, err := client.stub.VolumeRename(rpcCtx, pb.VolumeRenameRequest_builder{
		VolumeId: req.ID,
		Name:     req.Inputs.Name,
	}.Build()); err != nil {
		return infer.UpdateResponse[VolumeState]{}, fmt.Errorf("modal: rename volume %q to %q: %w", req.State.Name, req.Inputs.Name, err)
	}
	return infer.UpdateResponse[VolumeState]{Output: state}, nil
}

// Delete removes the Modal Volume and all of its data.
func (r *Volume) Delete(ctx context.Context, req infer.DeleteRequest[VolumeState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.VolumeDelete(rpcCtx, pb.VolumeDeleteRequest_builder{VolumeId: req.ID}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete volume %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
