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
// Dict resource
// ---------------------------------------------------------------------------

// Dict provisions a Modal Dict: a durable, named distributed key/value store
// shared between Modal Functions. The resource manages the object itself, not
// its entries.
type Dict struct{}

// DictArgs are the user-supplied inputs for a Modal Dict.
type DictArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the dict's unique name within its environment. Changing this
	// forces a new resource.
	Name string `pulumi:"name"`

	// Environment is the Modal environment the dict lives in. Changing this
	// forces a new resource. Falls back to the provider-level environment.
	Environment *string `pulumi:"environment,optional"`
}

// DictState holds the computed outputs for a Modal Dict.
type DictState struct {
	DictArgs

	// DictId is the Modal-assigned unique identifier for the dict.
	DictId string `pulumi:"dictId"`
}

func (r *Dict) Annotate(a infer.Annotator) {
	a.Describe(&r, "A Modal Dict — a durable, named distributed key/value store shared between Modal Functions. "+
		"The resource manages the Dict object, not its entries. Deleting it deletes every entry. "+
		"See https://modal.com/docs/guide/dicts.")
}

func (a *DictArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The dict's unique name within its environment. Changing this forces a new resource.")
	ann.Describe(&a.Environment, "The Modal environment the dict lives in. Changing this forces a new resource. "+
		"When unset, the provider-level environment or the account's default environment is used.")
}

func (s *DictState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.DictId, "The Modal-assigned identifier of the Dict.")
}

var _ infer.CustomResource[DictArgs, DictState] = (*Dict)(nil)
var _ infer.CustomDiff[DictArgs, DictState] = (*Dict)(nil)
var _ infer.CustomRead[DictArgs, DictState] = (*Dict)(nil)
var _ infer.CustomDelete[DictState] = (*Dict)(nil)

// Diff replaces on a name or environment change: Modal has no rename or
// update call for dicts.
func (r *Dict) Diff(ctx context.Context, req infer.DiffRequest[DictArgs, DictState]) (infer.DiffResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	if req.Inputs.Name != req.State.Name {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	if environmentChanged(req.Inputs.Environment, req.State.Environment, cfg) {
		resp.DetailedDiff["environment"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	return resp, nil
}

func (r *Dict) Create(ctx context.Context, req infer.CreateRequest[DictArgs]) (infer.CreateResponse[DictState], error) {
	args := req.Inputs
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		state := DictState{DictArgs: args}
		if env := resolveStr(args.Environment, cfg.Environment); env != "" {
			state.Environment = &env
		}
		return infer.CreateResponse[DictState]{ID: args.Name, Output: state}, nil
	}

	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[DictState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	environment := effectiveEnvironment(rpcCtx, client, args.Environment, cfg)
	resp, err := client.stub.DictGetOrCreate(rpcCtx, pb.DictGetOrCreateRequest_builder{
		DeploymentName:     args.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS,
	}.Build())
	if err != nil {
		return infer.CreateResponse[DictState]{}, fmt.Errorf("modal: create dict %q: %w", args.Name, err)
	}

	state := DictState{DictArgs: args, DictId: resp.GetDictId()}
	if environment != "" {
		state.Environment = &environment
	}
	return infer.CreateResponse[DictState]{ID: resp.GetDictId(), Output: state}, nil
}

// Read refreshes the dict by ID, so it works for refresh and for import from
// the ID alone. Modal does not report a dict's environment.
func (r *Dict) Read(ctx context.Context, req infer.ReadRequest[DictArgs, DictState]) (infer.ReadResponse[DictArgs, DictState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[DictArgs, DictState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.DictGetById(rpcCtx, pb.DictGetByIdRequest_builder{DictId: req.ID}.Build())
	if isNotFound(err) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("modal: read dict %q: %w", req.ID, err)
	}
	name := resp.GetMetadata().GetName()

	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	state.DictId = req.ID
	state.Name = name

	environment := derefStr(state.Environment)
	if environment == "" {
		environment = effectiveEnvironment(rpcCtx, client, nil, cfg)
	}
	if importing {
		lookup, lerr := client.stub.DictGetOrCreate(rpcCtx, pb.DictGetOrCreateRequest_builder{
			DeploymentName:     name,
			EnvironmentName:    environment,
			ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
		}.Build())
		if lerr != nil || lookup.GetDictId() != req.ID {
			return empty, fmt.Errorf("modal: dict %q (%s) was not found in environment %q; "+
				"set the modal:environment provider config (or MODAL_ENVIRONMENT) to the environment the dict lives in and retry the import",
				name, req.ID, environment)
		}
	}
	if environment != "" {
		state.Environment = &environment
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = name
	if importing && environment != "" {
		inputs.Environment = &environment
	}
	return infer.ReadResponse[DictArgs, DictState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

// Delete removes the Modal Dict and every entry in it.
func (r *Dict) Delete(ctx context.Context, req infer.DeleteRequest[DictState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.DictDelete(rpcCtx, pb.DictDeleteRequest_builder{DictId: req.ID}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete dict %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
