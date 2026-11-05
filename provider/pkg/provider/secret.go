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
	"slices"
	"sort"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
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

	// Keys are the names of the keys currently stored in the Secret, sorted.
	// Modal never returns the values, so keys are what refresh can compare.
	Keys []string `pulumi:"keys,optional"`
}

func (r *Secret) Annotate(a infer.Annotator) {
	a.Describe(&r, "A durable, named Modal Secret — a collection of key/value pairs Modal Functions mount as environment variables. "+
		"Modal never returns secret values, so refresh and import can only see key names: an imported Secret compares key names until "+
		"the program's values are recorded, so the first preview after an import is clean. See https://modal.com/docs/guide/secrets.")
}

func (a *SecretArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The unique Secret name. Changing this forces a new resource.")
	ann.Describe(&a.Environment, "The Modal environment the Secret lives in. Changing this forces a new resource. "+
		"When unset, the provider-level environment or the account's default environment is used.")
	ann.Describe(&a.Values, "Key/value pairs exposed as environment variables to Functions that mount this Secret. "+
		"Keys must be valid environment variable names (letters, digits and underscores, not starting with a digit).")
}

func (s *SecretState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.SecretId, "The Modal-assigned identifier of the Secret.")
	ann.Describe(&s.Keys, "Sorted names of the keys currently stored in the Secret, as last read from Modal.")
}

var _ infer.CustomResource[SecretArgs, SecretState] = (*Secret)(nil)
var _ infer.CustomDiff[SecretArgs, SecretState] = (*Secret)(nil)
var _ infer.CustomCheck[SecretArgs] = (*Secret)(nil)
var _ infer.CustomRead[SecretArgs, SecretState] = (*Secret)(nil)
var _ infer.CustomUpdate[SecretArgs, SecretState] = (*Secret)(nil)
var _ infer.CustomDelete[SecretState] = (*Secret)(nil)

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Check rejects invalid key names at preview time instead of at apply time.
func (r *Secret) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[SecretArgs], error) {
	args, failures, err := infer.DefaultCheck[SecretArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[SecretArgs]{}, err
	}
	for _, k := range sortedKeys(args.Values) {
		if verr := validateEnvVarName(k); verr != nil {
			failures = append(failures, pgp.CheckFailure{Property: "values", Reason: verr.Error()})
		}
	}
	return infer.CheckResponse[SecretArgs]{Inputs: args, Failures: failures}, nil
}

type oldInputsKey struct{}

// withOldInputs makes the engine-supplied old inputs available to Diff.
// pulumi-go-provider v1.3.1 drops DiffRequest.OldInputs before calling
// CustomDiff, which is the only place the values last written by the program
// survive for an imported Secret (see
// https://github.com/pulumi/pulumi-go-provider/issues/710).
func withOldInputs(ctx context.Context, old property.Map) context.Context {
	return context.WithValue(ctx, oldInputsKey{}, old)
}

// oldInputValues returns the "values" of the old inputs, or nil.
func oldInputValues(ctx context.Context) map[string]string {
	old, ok := ctx.Value(oldInputsKey{}).(property.Map)
	if !ok {
		return nil
	}
	v, ok := old.GetOk("values")
	if !ok || !v.IsMap() {
		return nil
	}
	out := map[string]string{}
	for k, e := range v.AsMap().All {
		if e.IsString() {
			out[k] = e.AsString()
		}
	}
	return out
}

// Diff forces replacement when the name or environment changes; value changes
// are handled in place via Update.
func (r *Secret) Diff(ctx context.Context, req infer.DiffRequest[SecretArgs, SecretState]) (infer.DiffResponse, error) {
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
	recorded := req.State.Values
	if len(recorded) == 0 {
		// An imported Secret has no recorded values (Modal never returns them);
		// fall back to the values from the last program run, if any.
		recorded = oldInputValues(ctx)
	}
	var valuesChanged bool
	if len(recorded) == 0 && len(req.State.Keys) > 0 {
		// Values are unknown (imported, never written by Pulumi): compare key
		// names only, so the first preview after an import is clean.
		valuesChanged = !slices.Equal(slices.Clone(req.State.Keys), sortedKeys(req.Inputs.Values))
	} else {
		valuesChanged = len(req.Inputs.Values) != len(recorded)
		if !valuesChanged {
			for k, v := range req.Inputs.Values {
				if old, ok := recorded[k]; !ok || old != v {
					valuesChanged = true
					break
				}
			}
		}
		// Drift: keys added or removed outside Pulumi show up in the refreshed keys.
		if !valuesChanged && req.State.Keys != nil &&
			!slices.Equal(slices.Clone(req.State.Keys), sortedKeys(req.Inputs.Values)) {
			valuesChanged = true
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
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		state := SecretState{SecretArgs: args, Keys: sortedKeys(args.Values)}
		if env := resolveEnv(args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret)); env != "" {
			state.Environment = &env
		}
		return infer.CreateResponse[SecretState]{ID: args.Name, Output: state}, nil
	}
	for k := range args.Values {
		if err := validateEnvVarName(k); err != nil {
			return infer.CreateResponse[SecretState]{}, err
		}
	}

	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[SecretState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	environment := effectiveEnvironment(rpcCtx, client, args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret))
	resp, err := client.stub.SecretGetOrCreate(rpcCtx, pb.SecretGetOrCreateRequest_builder{
		DeploymentName:     args.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS,
		EnvDict:            args.Values,
	}.Build())
	if err != nil {
		return infer.CreateResponse[SecretState]{}, fmt.Errorf("modal: create secret %q: %w", args.Name, err)
	}

	state := SecretState{SecretArgs: args, SecretId: resp.GetSecretId(), Keys: sortedKeys(args.Values)}
	if environment != "" {
		state.Environment = &environment
	}
	return infer.CreateResponse[SecretState]{ID: resp.GetSecretId(), Output: state}, nil
}

// Read refreshes the secret by ID: name, environment and key names. Values
// cannot be read back from Modal, so they stay as recorded in state.
func (r *Secret) Read(ctx context.Context, req infer.ReadRequest[SecretArgs, SecretState]) (infer.ReadResponse[SecretArgs, SecretState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[SecretArgs, SecretState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.SecretGetInfo(rpcCtx, pb.SecretGetInfoRequest_builder{SecretId: req.ID}.Build())
	if isNotFound(err) {
		// Deleted outside Pulumi: signal removal.
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("modal: read secret %q: %w", req.ID, err)
	}
	meta := resp.GetMetadata()

	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	state.SecretId = req.ID
	state.Name = meta.GetName()
	keys := slices.Clone(meta.GetKeys())
	sort.Strings(keys)
	state.Keys = keys
	environment := meta.GetEnvironmentName()
	if environment == "" {
		environment = derefStr(state.Environment)
	}
	if environment != "" {
		state.Environment = &environment
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = meta.GetName()
	if importing {
		// Values are required properties but Modal never returns them: import
		// them as empty. Diff then compares key names only until the program's
		// values are recorded as inputs.
		inputs.Values = map[string]string{}
		state.Values = map[string]string{}
		if environment != "" {
			inputs.Environment = &environment
		}
	}
	return infer.ReadResponse[SecretArgs, SecretState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

func (r *Secret) Update(ctx context.Context, req infer.UpdateRequest[SecretArgs, SecretState]) (infer.UpdateResponse[SecretState], error) {
	args := req.Inputs
	state := req.State
	state.SecretArgs = args
	state.Environment = req.State.Environment
	state.Keys = sortedKeys(args.Values)
	if req.DryRun {
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
	for _, k := range sortedKeys(args.Values) {
		val := args.Values[k]
		updates = append(updates, pb.SecretUpdateRequest_Update_builder{Key: k, Value: &val}.Build())
	}
	// Remove keys that exist (per state values or the last refresh) but are no
	// longer in the program.
	stale := map[string]bool{}
	for k := range req.State.Values {
		stale[k] = true
	}
	for _, k := range req.State.Keys {
		stale[k] = true
	}
	var staleKeys []string
	for k := range stale {
		if _, ok := args.Values[k]; !ok {
			staleKeys = append(staleKeys, k)
		}
	}
	sort.Strings(staleKeys)
	for _, k := range staleKeys {
		updates = append(updates, pb.SecretUpdateRequest_Update_builder{Key: k, Value: nil}.Build())
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
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete secret %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
