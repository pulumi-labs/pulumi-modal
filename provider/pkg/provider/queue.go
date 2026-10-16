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

// Diff returns a replace diff for the name and environment fields, which are
// immutable on the Modal control plane. There is no Update path for Queue.
func (r *Queue) Diff(ctx context.Context, req infer.DiffRequest[QueueArgs, QueueState]) (infer.DiffResponse, error) {
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
	return resp, nil
}

func (r *Queue) Create(ctx context.Context, req infer.CreateRequest[QueueArgs]) (infer.CreateResponse[QueueState], error) {
	args := req.Inputs
	if req.DryRun {
		return infer.CreateResponse[QueueState]{ID: args.Name, Output: QueueState{QueueArgs: args}}, nil
	}

	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[QueueState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	environment := resolveStr(args.Environment, cfg.Environment)
	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.QueueGetOrCreate(rpcCtx, pb.QueueGetOrCreateRequest_builder{
		DeploymentName:     args.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS,
	}.Build())
	if err != nil {
		return infer.CreateResponse[QueueState]{}, fmt.Errorf("modal: create queue %q: %w", args.Name, err)
	}

	return infer.CreateResponse[QueueState]{
		ID: resp.GetQueueId(),
		Output: QueueState{
			QueueArgs: args,
			QueueId:   resp.GetQueueId(),
		},
	}, nil
}

func (r *Queue) Read(ctx context.Context, req infer.ReadRequest[QueueArgs, QueueState]) (infer.ReadResponse[QueueArgs, QueueState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.ReadResponse[QueueArgs, QueueState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	environment := resolveStr(req.State.Environment, cfg.Environment)
	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.QueueGetOrCreate(rpcCtx, pb.QueueGetOrCreateRequest_builder{
		DeploymentName:     req.State.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
	}.Build())
	if status.Code(err) == codes.NotFound {
		// Deleted externally — signal removal.
		return infer.ReadResponse[QueueArgs, QueueState]{}, nil
	}
	if err != nil {
		return infer.ReadResponse[QueueArgs, QueueState]{}, fmt.Errorf("modal: read queue %q: %w", req.State.Name, err)
	}

	state := req.State
	state.QueueId = resp.GetQueueId()
	return infer.ReadResponse[QueueArgs, QueueState]{
		ID:     resp.GetQueueId(),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Delete removes the Modal Queue. All fields are immutable, so there is no
// Update path — any input change triggers a replace via Diff.
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
	if err != nil && status.Code(err) != codes.NotFound {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete queue %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
