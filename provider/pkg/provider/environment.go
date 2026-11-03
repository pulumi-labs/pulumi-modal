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
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// ---------------------------------------------------------------------------
// Environment resource
// ---------------------------------------------------------------------------

// Environment provisions a Modal environment: a sub-namespace of a workspace
// that holds Secrets, Volumes, Queues, Dicts, Apps and Images. Environments
// are workspace-level, so the resource has no environment input of its own.
type Environment struct{}

// EnvironmentArgs are the user-supplied inputs for a Modal environment.
type EnvironmentArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the environment's name, unique in the workspace. Changing it
	// renames the environment in place.
	Name string `pulumi:"name"`

	// WebSuffix is the suffix of web endpoint URLs created in this
	// environment. Modal picks one when unset.
	WebSuffix *string `pulumi:"webSuffix,optional"`

	// MaxConcurrentTasks caps the number of tasks running at once in this
	// environment. Unlimited when unset.
	MaxConcurrentTasks *int `pulumi:"maxConcurrentTasks,optional"`

	// MaxConcurrentGpus caps the number of GPUs in use at once in this
	// environment. Unlimited when unset.
	MaxConcurrentGpus *int `pulumi:"maxConcurrentGpus,optional"`
}

// EnvironmentState holds the computed outputs for a Modal environment.
type EnvironmentState struct {
	EnvironmentArgs

	// EnvironmentId is the Modal-assigned unique identifier.
	EnvironmentId string `pulumi:"environmentId"`

	// IsDefault reports whether this is the workspace's default environment.
	IsDefault bool `pulumi:"isDefault"`
}

func (r *Environment) Annotate(a infer.Annotator) {
	a.Describe(&r, "A Modal environment — a sub-namespace of a workspace that holds Secrets, Volumes, Queues, Dicts, "+
		"Apps and Images. Environments are workspace-level. Deleting one deletes everything in it. "+
		"See https://modal.com/docs/guide/environments.")
}

func (a *EnvironmentArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The environment's name, unique in the workspace. Changing this renames the environment in place.")
	ann.Describe(&a.WebSuffix, "Suffix of web endpoint URLs created in this environment. Modal assigns one when unset.")
	ann.Describe(&a.MaxConcurrentTasks, "Maximum number of tasks running at once in this environment. Unlimited when unset.")
	ann.Describe(&a.MaxConcurrentGpus, "Maximum number of GPUs in use at once in this environment. Unlimited when unset.")
}

func (s *EnvironmentState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.EnvironmentId, "The Modal-assigned identifier of the environment.")
	ann.Describe(&s.IsDefault, "Whether this is the workspace's default environment.")
}

var _ infer.CustomResource[EnvironmentArgs, EnvironmentState] = (*Environment)(nil)
var _ infer.CustomDiff[EnvironmentArgs, EnvironmentState] = (*Environment)(nil)
var _ infer.CustomUpdate[EnvironmentArgs, EnvironmentState] = (*Environment)(nil)
var _ infer.CustomRead[EnvironmentArgs, EnvironmentState] = (*Environment)(nil)
var _ infer.CustomDelete[EnvironmentState] = (*Environment)(nil)

func int32Ptr(v *int) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v)
	return &n
}

func optInt(set bool, v int32) *int {
	if !set {
		return nil
	}
	n := int(v)
	return &n
}

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		// An unset input means "leave as is", so it never differs.
		return a == nil
	}
	return *a == *b
}

// Diff reports every property change as an in-place update: Modal can rename
// an environment and change its web suffix and limits without replacing it.
func (r *Environment) Diff(_ context.Context, req infer.DiffRequest[EnvironmentArgs, EnvironmentState]) (infer.DiffResponse, error) {
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	mark := func(key string) {
		resp.DetailedDiff[key] = pgp.PropertyDiff{Kind: pgp.Update}
		resp.HasChanges = true
	}
	if req.Inputs.Name != req.State.Name {
		mark("name")
	}
	if !ptrEqual(req.Inputs.WebSuffix, req.State.WebSuffix) {
		mark("webSuffix")
	}
	if !ptrEqual(req.Inputs.MaxConcurrentTasks, req.State.MaxConcurrentTasks) {
		mark("maxConcurrentTasks")
	}
	if !ptrEqual(req.Inputs.MaxConcurrentGpus, req.State.MaxConcurrentGpus) {
		mark("maxConcurrentGpus")
	}
	return resp, nil
}

// findEnvironment looks an environment up by name or, when name is empty, ID.
func findEnvironment(ctx context.Context, client *modalClient, id, name string) (*pb.EnvironmentListItem, error) {
	list, err := client.stub.EnvironmentList(ctx, emptyReq())
	if err != nil {
		return nil, err
	}
	for _, item := range list.GetItems() {
		if (id != "" && item.GetEnvironmentId() == id) || (id == "" && item.GetName() == name) {
			return item, nil
		}
	}
	return nil, nil
}

func fillEnvironmentState(state *EnvironmentState, item *pb.EnvironmentListItem, keepInputs bool) {
	state.Name = item.GetName()
	state.EnvironmentId = item.GetEnvironmentId()
	state.IsDefault = item.GetDefault()
	suffix := item.GetWebhookSuffix()
	if suffix != "" {
		state.WebSuffix = &suffix
	}
	state.MaxConcurrentTasks = optInt(item.HasMaxConcurrentTasks(), item.GetMaxConcurrentTasks())
	state.MaxConcurrentGpus = optInt(item.HasMaxConcurrentGpus(), item.GetMaxConcurrentGpus())
}

func (r *Environment) Create(ctx context.Context, req infer.CreateRequest[EnvironmentArgs]) (infer.CreateResponse[EnvironmentState], error) {
	args := req.Inputs
	if req.DryRun {
		return infer.CreateResponse[EnvironmentState]{ID: args.Name, Output: EnvironmentState{EnvironmentArgs: args}}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[EnvironmentState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	if _, err := client.stub.EnvironmentCreate(rpcCtx, pb.EnvironmentCreateRequest_builder{
		Name: args.Name,
		Settings: pb.EnvironmentSettings_builder{
			WebhookSuffix:      derefStr(args.WebSuffix),
			MaxConcurrentTasks: int32Ptr(args.MaxConcurrentTasks),
			MaxConcurrentGpus:  int32Ptr(args.MaxConcurrentGpus),
		}.Build(),
	}.Build()); err != nil {
		return infer.CreateResponse[EnvironmentState]{}, fmt.Errorf("modal: create environment %q: %w", args.Name, err)
	}
	item, err := findEnvironment(rpcCtx, client, "", args.Name)
	if err != nil || item == nil {
		return infer.CreateResponse[EnvironmentState]{}, fmt.Errorf("modal: environment %q was created but could not be read back: %v", args.Name, err)
	}
	state := EnvironmentState{EnvironmentArgs: args}
	fillEnvironmentState(&state, item, true)
	return infer.CreateResponse[EnvironmentState]{ID: item.GetEnvironmentId(), Output: state}, nil
}

// Read refreshes the environment by ID (the list is the only lookup Modal
// offers), so refresh and import from the ID alone both work.
func (r *Environment) Read(ctx context.Context, req infer.ReadRequest[EnvironmentArgs, EnvironmentState]) (infer.ReadResponse[EnvironmentArgs, EnvironmentState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[EnvironmentArgs, EnvironmentState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	item, err := findEnvironment(rpcCtx, client, req.ID, "")
	if err != nil {
		return empty, fmt.Errorf("modal: read environment %q: %w", req.ID, err)
	}
	if item == nil {
		return empty, nil
	}
	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	fillEnvironmentState(&state, item, false)

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = item.GetName()
	if importing {
		inputs.WebSuffix = state.WebSuffix
		inputs.MaxConcurrentTasks = state.MaxConcurrentTasks
		inputs.MaxConcurrentGpus = state.MaxConcurrentGpus
	} else {
		// Keep an unset input unset, but track drift in what the program sets.
		if inputs.WebSuffix != nil {
			inputs.WebSuffix = state.WebSuffix
		}
		if inputs.MaxConcurrentTasks != nil {
			inputs.MaxConcurrentTasks = state.MaxConcurrentTasks
		}
		if inputs.MaxConcurrentGpus != nil {
			inputs.MaxConcurrentGpus = state.MaxConcurrentGpus
		}
	}
	return infer.ReadResponse[EnvironmentArgs, EnvironmentState]{ID: item.GetEnvironmentId(), Inputs: inputs, State: state}, nil
}

func (r *Environment) Update(ctx context.Context, req infer.UpdateRequest[EnvironmentArgs, EnvironmentState]) (infer.UpdateResponse[EnvironmentState], error) {
	args := req.Inputs
	state := req.State
	state.EnvironmentArgs = args
	if args.WebSuffix == nil {
		state.WebSuffix = req.State.WebSuffix
	}
	if req.DryRun {
		return infer.UpdateResponse[EnvironmentState]{Output: state}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[EnvironmentState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	update := pb.EnvironmentUpdateRequest_builder{CurrentName: req.State.Name}
	if args.Name != req.State.Name {
		update.Name = wrapperspb.String(args.Name)
	}
	if args.WebSuffix != nil && !ptrEqual(args.WebSuffix, req.State.WebSuffix) {
		update.WebSuffix = wrapperspb.String(*args.WebSuffix)
	}
	if args.MaxConcurrentTasks != nil && !ptrEqual(args.MaxConcurrentTasks, req.State.MaxConcurrentTasks) {
		update.MaxConcurrentTasks = int32Ptr(args.MaxConcurrentTasks)
	}
	if args.MaxConcurrentGpus != nil && !ptrEqual(args.MaxConcurrentGpus, req.State.MaxConcurrentGpus) {
		update.MaxConcurrentGpus = int32Ptr(args.MaxConcurrentGpus)
	}

	rpcCtx, cancel := callCtx()
	defer cancel()
	item, err := client.stub.EnvironmentUpdate(rpcCtx, update.Build())
	if err != nil {
		return infer.UpdateResponse[EnvironmentState]{}, fmt.Errorf("modal: update environment %q: %w", req.State.Name, err)
	}
	fillEnvironmentState(&state, item, true)
	state.EnvironmentArgs.TokenID, state.EnvironmentArgs.TokenSecret = args.TokenID, args.TokenSecret
	return infer.UpdateResponse[EnvironmentState]{Output: state}, nil
}

// Delete removes the environment and everything in it.
func (r *Environment) Delete(ctx context.Context, req infer.DeleteRequest[EnvironmentState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	// Resolve the current name by ID in case it was renamed outside Pulumi.
	name := req.State.Name
	if item, ferr := findEnvironment(rpcCtx, client, req.ID, ""); ferr == nil {
		if item == nil {
			return infer.DeleteResponse{}, nil
		}
		name = item.GetName()
	}
	if _, err := client.stub.EnvironmentDelete(rpcCtx, pb.EnvironmentDeleteRequest_builder{Name: name}.Build()); err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete environment %q: %w", name, err)
	}
	return infer.DeleteResponse{}, nil
}
