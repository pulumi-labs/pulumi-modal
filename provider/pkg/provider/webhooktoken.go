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
	"strings"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ---------------------------------------------------------------------------
// WebhookToken resource
// ---------------------------------------------------------------------------

// WebhookToken provisions a Modal proxy auth token: the credential a caller
// presents to a web endpoint that has requires_proxy_auth enabled.
type WebhookToken struct{}

// WebhookTokenArgs are the user-supplied inputs for a Modal proxy auth token.
type WebhookTokenArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the token's label. It can be changed in place.
	Name string `pulumi:"name"`
	// Scoped restricts the token to the environments it is attached to
	// instead of the whole workspace. Changing this forces a new resource.
	Scoped *bool `pulumi:"scoped,optional"`
}

// WebhookTokenState holds the computed outputs for a Modal proxy auth token.
type WebhookTokenState struct {
	WebhookTokenArgs

	// WebhookTokenId is the Modal-assigned identifier of the token (wk-...), sent as the Modal-Key header.
	WebhookTokenId string `pulumi:"webhookTokenId"`
	// WebhookTokenSecret is the token secret (sent as the Modal-Secret header). Modal only returns it when the token is created.
	WebhookTokenSecret *string `pulumi:"webhookTokenSecret,optional" provider:"secret"`
	// CreatedAt is when the token was created, as an RFC 3339 timestamp.
	CreatedAt *string `pulumi:"createdAt,optional"`
}

func (r *WebhookToken) Annotate(a infer.Annotator) {
	a.Describe(&r, "A Modal proxy auth token: the key and secret a caller sends in the Modal-Key and Modal-Secret headers to reach a "+
		"web endpoint that requires proxy authentication. Modal returns the secret only when the token is created, so it is kept in "+
		"state as a secret and cannot be recovered by an import. The name updates in place; changing scoped replaces the token (and "+
		"rotates it). Attaching a scoped token to environments is not modelled yet. See https://modal.com/docs/guide/webhook-proxy-auth.")
}

func (a *WebhookTokenArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "A label for the token. Updates in place.")
	ann.Describe(&a.Scoped, "Restrict the token to the environments it is attached to instead of the whole workspace. Defaults to false. "+
		"Changing this forces a new resource.")
}

func (s *WebhookTokenState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.WebhookTokenId, "The Modal-assigned identifier of the token, sent as the Modal-Key header.")
	ann.Describe(&s.WebhookTokenSecret, "The token secret, sent as the Modal-Secret header. Only available when Pulumi created the token; an imported token has none.")
	ann.Describe(&s.CreatedAt, "When the token was created, as an RFC 3339 timestamp.")
}

var _ infer.CustomResource[WebhookTokenArgs, WebhookTokenState] = (*WebhookToken)(nil)
var _ infer.CustomDiff[WebhookTokenArgs, WebhookTokenState] = (*WebhookToken)(nil)
var _ infer.CustomRead[WebhookTokenArgs, WebhookTokenState] = (*WebhookToken)(nil)
var _ infer.CustomUpdate[WebhookTokenArgs, WebhookTokenState] = (*WebhookToken)(nil)
var _ infer.CustomDelete[WebhookTokenState] = (*WebhookToken)(nil)

// Diff updates the name in place and replaces on a scope change.
func (r *WebhookToken) Diff(_ context.Context, req infer.DiffRequest[WebhookTokenArgs, WebhookTokenState]) (infer.DiffResponse, error) {
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	if req.Inputs.Name != req.State.Name {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.Update}
	}
	if boolOr(req.Inputs.Scoped) != boolOr(req.State.Scoped) {
		resp.DetailedDiff["scoped"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.DeleteBeforeReplace = false
	}
	resp.HasChanges = len(resp.DetailedDiff) > 0
	return resp, nil
}

func (r *WebhookToken) Create(ctx context.Context, req infer.CreateRequest[WebhookTokenArgs]) (infer.CreateResponse[WebhookTokenState], error) {
	args := req.Inputs
	if strings.TrimSpace(args.Name) == "" {
		return infer.CreateResponse[WebhookTokenState]{}, fmt.Errorf("modal: webhook token name must not be empty")
	}
	if req.DryRun {
		return infer.CreateResponse[WebhookTokenState]{ID: "", Output: WebhookTokenState{WebhookTokenArgs: args}}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[WebhookTokenState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.WebhookTokenCreate(rpcCtx, pb.WebhookTokenCreateRequest_builder{
		Name:   args.Name,
		Scoped: boolOr(args.Scoped),
	}.Build())
	if err != nil {
		return infer.CreateResponse[WebhookTokenState]{}, fmt.Errorf("modal: create webhook token %q: %w", args.Name, err)
	}
	secret := resp.GetTokenSecret()
	state := WebhookTokenState{WebhookTokenArgs: args, WebhookTokenId: resp.GetTokenId(), WebhookTokenSecret: &secret}
	// The create response carries no timestamp: read it back when the listing allows. A failure here is
	// not worth failing the create for (the token and its one-time secret exist), so createdAt stays unset
	// and the next refresh fills it in.
	if t, lerr := findWebhookToken(rpcCtx, client, resp.GetTokenId()); lerr == nil && t != nil && t.GetCreatedAt() > 0 {
		s := unixToRFC3339(t.GetCreatedAt())
		state.CreatedAt = &s
	}
	return infer.CreateResponse[WebhookTokenState]{ID: resp.GetTokenId(), Output: state}, nil
}

// findWebhookToken finds a token by ID in the workspace listing; nil means it no longer exists.
func findWebhookToken(ctx context.Context, client *modalClient, id string) (*pb.WebhookToken, error) {
	list, err := client.stub.WebhookTokenList(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	for _, t := range list.GetTokens() {
		if t.GetTokenId() == id {
			return t, nil
		}
	}
	return nil, nil
}

// Read refreshes the token by ID, so it works for refresh and import. The secret is never returned by
// Modal after creation, so the stored one is kept.
func (r *WebhookToken) Read(ctx context.Context, req infer.ReadRequest[WebhookTokenArgs, WebhookTokenState]) (infer.ReadResponse[WebhookTokenArgs, WebhookTokenState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[WebhookTokenArgs, WebhookTokenState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	t, err := findWebhookToken(rpcCtx, client, req.ID)
	if err != nil {
		return empty, fmt.Errorf("modal: read webhook token %q: %w", req.ID, err)
	}
	if t == nil {
		return empty, nil
	}

	state := req.State
	state.WebhookTokenId = req.ID
	state.Name = t.GetName()
	scoped := t.GetScoped()
	if scoped || state.Scoped != nil {
		state.Scoped = &scoped
	}
	if t.GetCreatedAt() > 0 {
		s := unixToRFC3339(t.GetCreatedAt())
		state.CreatedAt = &s
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = state.Name
	inputs.Scoped = state.Scoped
	state.WebhookTokenArgs = inputs
	return infer.ReadResponse[WebhookTokenArgs, WebhookTokenState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

func (r *WebhookToken) Update(ctx context.Context, req infer.UpdateRequest[WebhookTokenArgs, WebhookTokenState]) (infer.UpdateResponse[WebhookTokenState], error) {
	args := req.Inputs
	state := req.State
	state.WebhookTokenArgs = args
	if strings.TrimSpace(args.Name) == "" {
		return infer.UpdateResponse[WebhookTokenState]{}, fmt.Errorf("modal: webhook token name must not be empty")
	}
	if req.DryRun {
		return infer.UpdateResponse[WebhookTokenState]{Output: state}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[WebhookTokenState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	name := args.Name
	if _, err := client.stub.WebhookTokenUpdate(rpcCtx, pb.WebhookTokenUpdateRequest_builder{TokenId: req.ID, Name: &name}.Build()); err != nil {
		return infer.UpdateResponse[WebhookTokenState]{}, fmt.Errorf("modal: update webhook token %q: %w", req.ID, err)
	}
	return infer.UpdateResponse[WebhookTokenState]{Output: state}, nil
}

func (r *WebhookToken) Delete(ctx context.Context, req infer.DeleteRequest[WebhookTokenState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.WebhookTokenDelete(rpcCtx, pb.TokenDeleteRequest_builder{TokenId: req.ID}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete webhook token %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// ---------------------------------------------------------------------------
// getWebhookToken function
// ---------------------------------------------------------------------------

// GetWebhookToken looks up a proxy auth token by name. It never returns the secret.
type GetWebhookToken struct{}

// GetWebhookTokenArgs selects the token.
type GetWebhookTokenArgs struct {
	// Name is the token's label.
	Name string `pulumi:"name"`
}

// GetWebhookTokenResult is the looked-up token.
type GetWebhookTokenResult struct {
	WebhookTokenId string `pulumi:"webhookTokenId"`
	Name           string `pulumi:"name"`
	Scoped         bool   `pulumi:"scoped"`
	CreatedAt      string `pulumi:"createdAt"`
}

func (a *GetWebhookTokenArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.Name, "The token's name. Fails when more than one token has this name.")
}

func (r *GetWebhookTokenResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.WebhookTokenId, "The Modal-assigned identifier of the token, sent as the Modal-Key header. The secret is never returned.")
	ann.Describe(&r.Name, "The token's name.")
	ann.Describe(&r.Scoped, "Whether the token is restricted to the environments it is attached to.")
	ann.Describe(&r.CreatedAt, "When the token was created, as an RFC 3339 timestamp (empty when Modal reports none).")
}

func (f *GetWebhookToken) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal proxy auth token by name. The secret is never returned.")
}

func (f *GetWebhookToken) Invoke(ctx context.Context, req infer.FunctionRequest[GetWebhookTokenArgs]) (infer.FunctionResponse[GetWebhookTokenResult], error) {
	out := infer.FunctionResponse[GetWebhookTokenResult]{}
	client, _, closeFn, err := lookupSession(ctx, nil)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	list, err := client.stub.WebhookTokenList(rpcCtx, &emptypb.Empty{})
	if err != nil {
		return out, fmt.Errorf("modal: list webhook tokens: %w", err)
	}
	var found []*pb.WebhookToken
	for _, t := range list.GetTokens() {
		if t.GetName() == req.Input.Name {
			found = append(found, t)
		}
	}
	switch len(found) {
	case 0:
		return out, fmt.Errorf("modal: webhook token %q was not found", req.Input.Name)
	case 1:
	default:
		return out, fmt.Errorf("modal: %d webhook tokens are named %q; names are not unique, look the token up through the resource ID instead", len(found), req.Input.Name)
	}
	t := found[0]
	created := ""
	if t.GetCreatedAt() > 0 {
		created = unixToRFC3339(t.GetCreatedAt())
	}
	out.Output = GetWebhookTokenResult{WebhookTokenId: t.GetTokenId(), Name: t.GetName(), Scoped: t.GetScoped(), CreatedAt: created}
	return out, nil
}
