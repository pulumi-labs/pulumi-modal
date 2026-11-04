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
// Queue resource
// ---------------------------------------------------------------------------

// Queue provisions a Modal Queue — a distributed, FIFO/LIFO queue for passing
// data between Modal Functions, Sandboxes, and external callers.
type Queue struct{}

// QueueArgs are the user-supplied inputs for a Modal Queue.
type QueueArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the queue's unique name within its environment. Changing this
	// forces a new resource.
	Name string `pulumi:"name"`

	// Environment is the Modal environment (workspace namespace) the queue
	// lives in. Changing this forces a new resource. Falls back to the
	// provider-level environment.
	Environment *string `pulumi:"environment,optional"`
}

// QueueState holds the computed outputs for a Modal Queue.
type QueueState struct {
	QueueArgs

	// QueueId is the Modal-assigned unique identifier for the queue
	// (distinct from its human-readable Name).
	QueueId string `pulumi:"queueId"`
}

func (r *Queue) Annotate(a infer.Annotator) {
	a.Describe(&r, "A Modal Queue — a distributed FIFO/LIFO queue for passing data between Modal "+
		"Functions, Sandboxes, and external callers. See https://modal.com/docs/guide/queues.")
}

func (a *QueueArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The queue's unique name. Changing this forces a new resource.")
	ann.Describe(&a.Environment, "The Modal environment the queue lives in. Changing this forces a new resource.")
}

var _ infer.CustomResource[QueueArgs, QueueState] = (*Queue)(nil)
var _ infer.CustomDiff[QueueArgs, QueueState] = (*Queue)(nil)
var _ infer.CustomRead[QueueArgs, QueueState] = (*Queue)(nil)
var _ infer.CustomDelete[QueueState] = (*Queue)(nil)

// Diff returns a replace diff for the name and environment fields, which are
// immutable on the Modal control plane (the API has no rename or update call
// for queues).
func (r *Queue) Diff(ctx context.Context, req infer.DiffRequest[QueueArgs, QueueState]) (infer.DiffResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	if req.Inputs.Name != req.State.Name {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	if environmentChanged(req.Inputs.Environment, req.State.Environment, withResourceTokens(cfg, req.Inputs.TokenID, req.Inputs.TokenSecret)) {
		resp.DetailedDiff["environment"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	return resp, nil
}

func (r *Queue) Create(ctx context.Context, req infer.CreateRequest[QueueArgs]) (infer.CreateResponse[QueueState], error) {
	args := req.Inputs
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		state := QueueState{QueueArgs: args}
		if env := resolveEnv(args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret)); env != "" {
			state.Environment = &env
		}
		return infer.CreateResponse[QueueState]{ID: args.Name, Output: state}, nil
	}

	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[QueueState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	environment := effectiveEnvironment(rpcCtx, client, args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret))
	resp, err := client.stub.QueueGetOrCreate(rpcCtx, pb.QueueGetOrCreateRequest_builder{
		DeploymentName:     args.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS,
	}.Build())
	if err != nil {
		return infer.CreateResponse[QueueState]{}, fmt.Errorf("modal: create queue %q: %w", args.Name, err)
	}

	state := QueueState{QueueArgs: args, QueueId: resp.GetQueueId()}
	if environment != "" {
		state.Environment = &environment
	}
	return infer.CreateResponse[QueueState]{ID: resp.GetQueueId(), Output: state}, nil
}

// Read refreshes the queue by ID, so it works for refresh and for import from
// the ID alone.
func (r *Queue) Read(ctx context.Context, req infer.ReadRequest[QueueArgs, QueueState]) (infer.ReadResponse[QueueArgs, QueueState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[QueueArgs, QueueState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.QueueGetById(rpcCtx, pb.QueueGetByIdRequest_builder{QueueId: req.ID}.Build())
	if isNotFound(err) {
		// Deleted outside Pulumi: signal removal.
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("modal: read queue %q: %w", req.ID, err)
	}
	meta := resp.GetMetadata()

	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	state.QueueId = req.ID
	state.Name = meta.GetName()

	// Modal does not report a queue's environment, so keep what is recorded.
	environment := derefStr(state.Environment)
	if environment == "" {
		environment = effectiveEnvironment(rpcCtx, client, nil, withResourceTokens(cfg, req.State.TokenID, req.State.TokenSecret))
	}
	if importing {
		lookup, lerr := client.stub.QueueGetOrCreate(rpcCtx, pb.QueueGetOrCreateRequest_builder{
			DeploymentName:     meta.GetName(),
			EnvironmentName:    environment,
			ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
		}.Build())
		if lerr != nil || lookup.GetQueueId() != req.ID {
			return empty, fmt.Errorf("modal: queue %q (%s) was not found in environment %q; "+
				"set the modal:environment provider config (or MODAL_ENVIRONMENT) to the environment the queue lives in and retry the import",
				meta.GetName(), req.ID, environment)
		}
	}
	if environment != "" {
		state.Environment = &environment
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = meta.GetName()
	if importing && environment != "" {
		inputs.Environment = &environment
	}
	return infer.ReadResponse[QueueArgs, QueueState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

// Delete removes the Modal Queue.
func (r *Queue) Delete(ctx context.Context, req infer.DeleteRequest[QueueState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.QueueDelete(rpcCtx, pb.QueueDeleteRequest_builder{QueueId: req.ID}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete queue %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
