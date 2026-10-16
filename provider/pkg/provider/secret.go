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
// Secret resource
// ---------------------------------------------------------------------------

// Secret provisions a durable, named Modal Secret: a collection of key/value
// pairs Modal Functions can mount as environment variables.
type Secret struct{}

// SecretArgs are the user-supplied inputs for a Modal Secret.
type SecretArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the unique Secret name within its environment. Changing this
	// forces a new resource.
	Name string `pulumi:"name"`

	// Environment is the Modal environment (workspace namespace) the Secret
	// lives in. Changing this forces a new resource. Falls back to the
	// provider-level environment.
	Environment *string `pulumi:"environment,optional"`

	// Values are the key/value pairs stored in the Secret and exposed as
	// environment variables to Functions that mount it. Keys must be valid
	// environment variable names. Changing values updates the Secret in place.
	Values map[string]string `pulumi:"values" provider:"secret"`
}

// SecretState holds the computed outputs for a Modal Secret.
type SecretState struct {
	SecretArgs

	// SecretId is the Modal-assigned unique identifier for the secret
	// (distinct from its human-readable Name).
	SecretId string `pulumi:"secretId"`
}

func (r *Secret) Annotate(a infer.Annotator) {
	a.Describe(&r, "A durable, named Modal Secret — a collection of key/value pairs Modal Functions mount as environment variables. See https://modal.com/docs/guide/secrets.")
}

func (a *SecretArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The unique Secret name. Changing this forces a new resource.")
	ann.Describe(&a.Environment, "The Modal environment the Secret lives in. Changing this forces a new resource.")
	ann.Describe(&a.Values, "Key/value pairs exposed as environment variables to Functions that mount this Secret.")
}

var _ infer.CustomResource[SecretArgs, SecretState] = (*Secret)(nil)
var _ infer.CustomDiff[SecretArgs, SecretState] = (*Secret)(nil)

// Diff forces replacement when the name or environment changes; value changes
// are handled in place via SecretUpdate.
func (r *Secret) Diff(ctx context.Context, req infer.DiffRequest[SecretArgs, SecretState]) (infer.DiffResponse, error) {
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
	valuesChanged := len(req.Inputs.Values) != len(req.State.Values)
	if !valuesChanged {
		for k, v := range req.Inputs.Values {
			if old, ok := req.State.Values[k]; !ok || old != v {
				valuesChanged = true
				break
			}
		}
	}
	if valuesChanged {
		resp.DetailedDiff["values"] = pgp.PropertyDiff{Kind: pgp.Update}
		resp.HasChanges = true
	}
	return resp, nil
}

func (r *Secret) Create(ctx context.Context, req infer.CreateRequest[SecretArgs]) (infer.CreateResponse[SecretState], error) {
	args := req.Inputs
	if req.DryRun {
		return infer.CreateResponse[SecretState]{ID: args.Name, Output: SecretState{SecretArgs: args}}, nil
	}
	for k := range args.Values {
		if err := validateEnvVarName(k); err != nil {
			return infer.CreateResponse[SecretState]{}, err
		}
	}

	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[SecretState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	environment := resolveStr(args.Environment, cfg.Environment)
	rpcCtx, cancel := callCtx()
	defer cancel()

	resp, err := client.stub.SecretGetOrCreate(rpcCtx, pb.SecretGetOrCreateRequest_builder{
		DeploymentName:     args.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS,
		EnvDict:            args.Values,
	}.Build())
	if err != nil {
		return infer.CreateResponse[SecretState]{}, fmt.Errorf("modal: create secret %q: %w", args.Name, err)
	}

	return infer.CreateResponse[SecretState]{
		ID:     resp.GetSecretId(),
		Output: SecretState{SecretArgs: args, SecretId: resp.GetSecretId()},
	}, nil
}

func (r *Secret) Read(ctx context.Context, req infer.ReadRequest[SecretArgs, SecretState]) (infer.ReadResponse[SecretArgs, SecretState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.ReadResponse[SecretArgs, SecretState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	environment := resolveStr(req.State.Environment, cfg.Environment)
	rpcCtx, cancel := callCtx()
	defer cancel()

	resp, err := client.stub.SecretGetOrCreate(rpcCtx, pb.SecretGetOrCreateRequest_builder{
		DeploymentName:     req.State.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
	}.Build())
	if status.Code(err) == codes.NotFound {
		// Deleted externally — signal removal.
		return infer.ReadResponse[SecretArgs, SecretState]{}, nil
	}
	if err != nil {
		return infer.ReadResponse[SecretArgs, SecretState]{}, fmt.Errorf("modal: read secret %q: %w", req.State.Name, err)
	}

	state := req.State
	state.SecretId = resp.GetSecretId()

	return infer.ReadResponse[SecretArgs, SecretState]{
		ID:     resp.GetSecretId(),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

func (r *Secret) Update(ctx context.Context, req infer.UpdateRequest[SecretArgs, SecretState]) (infer.UpdateResponse[SecretState], error) {
	args := req.Inputs
	if req.DryRun {
		state := req.State
		state.SecretArgs = args
		return infer.UpdateResponse[SecretState]{Output: state}, nil
	}
	for k := range args.Values {
		if err := validateEnvVarName(k); err != nil {
			return infer.UpdateResponse[SecretState]{}, err
		}
	}

	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[SecretState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	var updates []*pb.SecretUpdateRequest_Update
	for k, v := range args.Values {
		val := v
		updates = append(updates, pb.SecretUpdateRequest_Update_builder{Key: k, Value: &val}.Build())
	}
	// Remove keys present in state but no longer in inputs.
	for k := range req.State.Values {
		if _, ok := args.Values[k]; !ok {
			updates = append(updates, pb.SecretUpdateRequest_Update_builder{Key: k, Value: nil}.Build())
		}
	}

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.SecretUpdate(rpcCtx, pb.SecretUpdateRequest_builder{
		SecretId: req.ID,
		Updates:  updates,
	}.Build())
	if err != nil {
		return infer.UpdateResponse[SecretState]{}, fmt.Errorf("modal: update secret %q: %w", req.State.Name, err)
	}

	state := req.State
	state.SecretArgs = args
	return infer.UpdateResponse[SecretState]{Output: state}, nil
}

func (r *Secret) Delete(ctx context.Context, req infer.DeleteRequest[SecretState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.SecretDelete(rpcCtx, pb.SecretDeleteRequest_builder{SecretId: req.ID}.Build())
	if err != nil && status.Code(err) != codes.NotFound {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete secret %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
