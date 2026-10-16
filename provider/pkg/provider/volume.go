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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	// Name is the volume's unique name within its environment. Changing this
	// forces a new resource.
	Name string `pulumi:"name"`

	// Environment is the Modal environment (workspace namespace) the volume
	// lives in. Changing this forces a new resource. Falls back to the
	// provider-level environment.
	Environment *string `pulumi:"environment,optional"`

	// Version selects the volume filesystem version: "v1" (legacy) or "v2"
	// (default, recommended for new volumes). Changing this forces a new
	// resource.
	Version *string `pulumi:"version,optional"`
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
	ann.Describe(&a.Name, "The volume's unique name. Changing this forces a new resource.")
	ann.Describe(&a.Environment, "The Modal environment the volume lives in. Changing this forces a new resource.")
	ann.Describe(&a.Version, "Volume filesystem version: \"v1\" or \"v2\" (default). Changing this forces a new resource.")
}

var _ infer.CustomResource[VolumeArgs, VolumeState] = (*Volume)(nil)
var _ infer.CustomDiff[VolumeArgs, VolumeState] = (*Volume)(nil)

func volumeFsVersion(v *string) (pb.VolumeFsVersion, error) {
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
			fmt.Errorf("modal: volume version must be \"v1\" or \"v2\", got %q", *v)
	}
}

// Diff returns a replace diff for the name, environment, and version fields,
// all of which are immutable on the Modal control plane. There is no Update
// path for Volume.
func (r *Volume) Diff(ctx context.Context, req infer.DiffRequest[VolumeArgs, VolumeState]) (infer.DiffResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	if req.Inputs.Name != req.State.Name {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	if resolveStr(req.Inputs.Environment, cfg.Environment) != resolveStr(req.State.Environment, cfg.Environment) {
		resp.DetailedDiff["environment"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	if resolveStr(req.Inputs.Version, nil) != resolveStr(req.State.Version, nil) {
		resp.DetailedDiff["version"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	return resp, nil
}

func (r *Volume) Create(ctx context.Context, req infer.CreateRequest[VolumeArgs]) (infer.CreateResponse[VolumeState], error) {
	args := req.Inputs
	if req.DryRun {
		return infer.CreateResponse[VolumeState]{ID: args.Name, Output: VolumeState{VolumeArgs: args}}, nil
	}

	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[VolumeState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	environment := resolveStr(args.Environment, cfg.Environment)
	version, err := volumeFsVersion(args.Version)
	if err != nil {
		return infer.CreateResponse[VolumeState]{}, err
	}

	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.VolumeGetOrCreate(rpcCtx, pb.VolumeGetOrCreateRequest_builder{
		DeploymentName:     args.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS,
		Version:            version,
	}.Build())
	if err != nil {
		return infer.CreateResponse[VolumeState]{}, fmt.Errorf("modal: create volume %q: %w", args.Name, err)
	}

	return infer.CreateResponse[VolumeState]{
		ID: resp.GetVolumeId(),
		Output: VolumeState{
			VolumeArgs: args,
			VolumeId:   resp.GetVolumeId(),
		},
	}, nil
}

func (r *Volume) Read(ctx context.Context, req infer.ReadRequest[VolumeArgs, VolumeState]) (infer.ReadResponse[VolumeArgs, VolumeState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.ReadResponse[VolumeArgs, VolumeState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	environment := resolveStr(req.State.Environment, cfg.Environment)
	version, err := volumeFsVersion(req.State.Version)
	if err != nil {
		return infer.ReadResponse[VolumeArgs, VolumeState]{}, err
	}

	rpcCtx, cancel := callCtx()
	defer cancel()
	// Modal's get-or-create RPC is also the lookup primitive: UNSPECIFIED
	// creation type performs a lookup without mutating state if the volume
	// already exists.
	resp, err := client.stub.VolumeGetOrCreate(rpcCtx, pb.VolumeGetOrCreateRequest_builder{
		DeploymentName:     req.State.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
		Version:            version,
	}.Build())
	if status.Code(err) == codes.NotFound {
		// Deleted externally — signal removal.
		return infer.ReadResponse[VolumeArgs, VolumeState]{}, nil
	}
	if err != nil {
		return infer.ReadResponse[VolumeArgs, VolumeState]{}, fmt.Errorf("modal: read volume %q: %w", req.State.Name, err)
	}

	state := req.State
	state.VolumeId = resp.GetVolumeId()
	return infer.ReadResponse[VolumeArgs, VolumeState]{
		ID:     resp.GetVolumeId(),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Delete removes the Modal Volume. All fields are immutable, so there is no
// Update path — any input change triggers a replace via Diff.
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
	if err != nil && status.Code(err) != codes.NotFound {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete volume %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
