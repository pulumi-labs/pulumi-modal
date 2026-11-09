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
	"time"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ---------------------------------------------------------------------------
// ServiceUser resource
// ---------------------------------------------------------------------------

// ServiceUser provisions a Modal service user: a non-human workspace identity
// with its own API token, meant for CI/CD and other automation.
type ServiceUser struct{}

// ServiceUserArgs are the user-supplied inputs for a Modal service user.
type ServiceUserArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the service user's name, unique within the workspace. Changing
	// this forces a new resource.
	Name string `pulumi:"name"`
	// Scoped restricts the service user to the environments it is granted a
	// role in, instead of the whole workspace. Changing this forces a new
	// resource.
	Scoped *bool `pulumi:"scoped,optional"`
	// ExpiresInSeconds makes the service user's token expire after this many
	// seconds. Unset means it never expires. Changing this forces a new
	// resource.
	ExpiresInSeconds *int `pulumi:"expiresInSeconds,optional"`
}

// ServiceUserState holds the computed outputs for a Modal service user.
type ServiceUserState struct {
	ServiceUserArgs

	// ServiceUserId is the Modal-assigned identifier of the service user.
	ServiceUserId string `pulumi:"serviceUserId"`
	// ServiceTokenId is the ID of the service user's own API token.
	ServiceTokenId string `pulumi:"serviceTokenId"`
	// ServiceTokenSecret is the service user's API token secret. Modal only
	// returns it when the service user is created.
	ServiceTokenSecret *string `pulumi:"serviceTokenSecret,optional" provider:"secret"`
	// ExpiresAt is the token's expiry as an RFC 3339 timestamp, when it has one.
	ExpiresAt *string `pulumi:"expiresAt,optional"`
}

func (r *ServiceUser) Annotate(a infer.Annotator) {
	a.Describe(&r, "A Modal service user: a non-human workspace identity with its own API token, for CI/CD and automation. "+
		"Modal returns the token secret only when the service user is created, so it is kept in state as a secret and cannot be "+
		"recovered by an import. Modal has no update call, so every input change replaces the service user (and rotates its token). "+
		"See https://modal.com/docs/guide/service-users.")
}

func (a *ServiceUserArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The service user's name, unique within the workspace. Changing this forces a new resource.")
	ann.Describe(&a.Scoped, "Restrict the service user to the environments it is granted a role in instead of the whole workspace. "+
		"Defaults to false. Changing this forces a new resource.")
	ann.Describe(&a.ExpiresInSeconds, "Expire the service user's token after this many seconds. Unset means it never expires. "+
		"Changing this forces a new resource.")
}

func (s *ServiceUserState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.ServiceUserId, "The Modal-assigned identifier of the service user.")
	ann.Describe(&s.ServiceTokenId, "The ID of the service user's own API token, to use as MODAL_TOKEN_ID.")
	ann.Describe(&s.ServiceTokenSecret, "The service user's API token secret, to use as MODAL_TOKEN_SECRET. Only available "+
		"when Pulumi created the service user; an imported service user has none.")
	ann.Describe(&s.ExpiresAt, "When the service user's token expires, as an RFC 3339 timestamp. Unset when it never expires.")
}

var _ infer.CustomResource[ServiceUserArgs, ServiceUserState] = (*ServiceUser)(nil)
var _ infer.CustomDiff[ServiceUserArgs, ServiceUserState] = (*ServiceUser)(nil)
var _ infer.CustomRead[ServiceUserArgs, ServiceUserState] = (*ServiceUser)(nil)
var _ infer.CustomDelete[ServiceUserState] = (*ServiceUser)(nil)

func unixToRFC3339(sec float64) string {
	return time.Unix(int64(sec), 0).UTC().Format(time.RFC3339)
}

func boolOr(p *bool) bool { return p != nil && *p }

func intOr(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// Diff replaces on every input change: Modal has no update call for service
// users.
func (r *ServiceUser) Diff(_ context.Context, req infer.DiffRequest[ServiceUserArgs, ServiceUserState]) (infer.DiffResponse, error) {
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	if req.Inputs.Name != req.State.Name {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	if boolOr(req.Inputs.Scoped) != boolOr(req.State.Scoped) {
		resp.DetailedDiff["scoped"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	if intOr(req.Inputs.ExpiresInSeconds) != intOr(req.State.ExpiresInSeconds) {
		resp.DetailedDiff["expiresInSeconds"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	resp.HasChanges = len(resp.DetailedDiff) > 0
	return resp, nil
}

func (r *ServiceUser) Create(ctx context.Context, req infer.CreateRequest[ServiceUserArgs]) (infer.CreateResponse[ServiceUserState], error) {
	args := req.Inputs
	if args.ExpiresInSeconds != nil && *args.ExpiresInSeconds < 0 {
		return infer.CreateResponse[ServiceUserState]{}, fmt.Errorf("modal: expiresInSeconds must not be negative")
	}
	if req.DryRun {
		return infer.CreateResponse[ServiceUserState]{ID: "", Output: ServiceUserState{ServiceUserArgs: args}}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[ServiceUserState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.ServiceUserCreate(rpcCtx, pb.ServiceUserCreateRequest_builder{
		Name:             args.Name,
		Scoped:           boolOr(args.Scoped),
		ExpiresInSeconds: uint32(intOr(args.ExpiresInSeconds)),
	}.Build())
	if err != nil {
		return infer.CreateResponse[ServiceUserState]{}, fmt.Errorf("modal: create service user %q: %w", args.Name, err)
	}
	secret := resp.GetTokenSecret()
	state := ServiceUserState{
		ServiceUserArgs:    args,
		ServiceUserId:      resp.GetServiceUserId(),
		ServiceTokenId:     resp.GetTokenId(),
		ServiceTokenSecret: &secret,
	}
	if resp.HasExpiresAt() {
		s := resp.GetExpiresAt().AsTime().UTC().Format(time.RFC3339)
		state.ExpiresAt = &s
	}
	return infer.CreateResponse[ServiceUserState]{ID: resp.GetServiceUserId(), Output: state}, nil
}

// findServiceUser finds a service user by ID in the workspace listing; nil
// means it no longer exists.
func findServiceUser(ctx context.Context, client *modalClient, id string) (*pb.ServiceUser, error) {
	list, err := client.stub.ServiceUserList(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	for _, u := range list.GetServiceUsers() {
		if u.GetServiceUserId() == id {
			return u, nil
		}
	}
	return nil, nil
}

// Read refreshes the service user by ID, so it works for refresh and import.
// The token secret is never returned by Modal after creation, so the stored
// one is kept.
func (r *ServiceUser) Read(ctx context.Context, req infer.ReadRequest[ServiceUserArgs, ServiceUserState]) (infer.ReadResponse[ServiceUserArgs, ServiceUserState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[ServiceUserArgs, ServiceUserState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	u, err := findServiceUser(rpcCtx, client, req.ID)
	if err != nil {
		return empty, fmt.Errorf("modal: read service user %q: %w", req.ID, err)
	}
	if u == nil {
		if err := notFoundOnImport("service user", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}

	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	state.ServiceUserId = req.ID
	state.Name = u.GetName()
	state.ServiceTokenId = u.GetTokenId()
	scoped := u.GetScoped()
	if scoped || state.Scoped != nil {
		state.Scoped = &scoped
	}
	if u.HasExpiresAt() && u.GetExpiresAt() > 0 {
		s := unixToRFC3339(u.GetExpiresAt())
		state.ExpiresAt = &s
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = state.Name
	inputs.Scoped = state.Scoped
	if importing {
		inputs.ExpiresInSeconds = nil
	} else {
		inputs.ExpiresInSeconds = state.ExpiresInSeconds
	}
	state.ExpiresInSeconds = inputs.ExpiresInSeconds
	return infer.ReadResponse[ServiceUserArgs, ServiceUserState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

func (r *ServiceUser) Delete(ctx context.Context, req infer.DeleteRequest[ServiceUserState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.ServiceUserDelete(rpcCtx, pb.ServiceUserDeleteRequest_builder{ServiceUserId: req.ID}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete service user %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// ---------------------------------------------------------------------------
// getServiceUser function
// ---------------------------------------------------------------------------

// GetServiceUser looks up a service user by name. It never returns a token.
type GetServiceUser struct{}

// GetServiceUserArgs selects the service user.
type GetServiceUserArgs struct {
	// Name is the service user's name.
	Name string `pulumi:"name"`
}

// GetServiceUserResult is the looked-up service user.
type GetServiceUserResult struct {
	ServiceUserId  string `pulumi:"serviceUserId"`
	Name           string `pulumi:"name"`
	ServiceTokenId string `pulumi:"serviceTokenId"`
	Scoped         bool   `pulumi:"scoped"`
	Status         string `pulumi:"status"`
}

func (a *GetServiceUserArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.Name, "The service user's name.")
}

func (r *GetServiceUserResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.ServiceUserId, "The Modal-assigned identifier of the service user.")
	ann.Describe(&r.Name, "The service user's name.")
	ann.Describe(&r.ServiceTokenId, "The ID of the service user's API token. The secret is never returned.")
	ann.Describe(&r.Scoped, "Whether the service user is restricted to the environments it holds a role in.")
	ann.Describe(&r.Status, "The service user's status as reported by Modal.")
}

func (f *GetServiceUser) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal service user by name. The token secret is never returned.")
}

func (f *GetServiceUser) Invoke(ctx context.Context, req infer.FunctionRequest[GetServiceUserArgs]) (infer.FunctionResponse[GetServiceUserResult], error) {
	out := infer.FunctionResponse[GetServiceUserResult]{}
	client, _, closeFn, err := lookupSession(ctx, nil)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := client.stub.ServiceUserInfo(rpcCtx, pb.ServiceUserInfoRequest_builder{ServiceUserName: req.Input.Name}.Build())
	if err != nil {
		return out, lookupErr("service user", req.Input.Name, "", err)
	}
	u := got.GetServiceUser()
	out.Output = GetServiceUserResult{
		ServiceUserId: u.GetServiceUserId(), Name: u.GetName(), ServiceTokenId: u.GetTokenId(),
		Scoped: u.GetScoped(), Status: u.GetStatus(),
	}
	return out, nil
}
