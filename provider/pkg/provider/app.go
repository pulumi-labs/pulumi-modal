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
	"maps"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ---------------------------------------------------------------------------
// App resource
// ---------------------------------------------------------------------------

// App provisions a named Modal App: the container that Functions, Images and
// Sandboxes belong to. The resource creates the empty, named App; it does not
// deploy code (`modal deploy` packages Python code, which Pulumi cannot do).
type App struct{}

// AppArgs are the user-supplied inputs for a Modal App.
type AppArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the app's unique name within its environment. Changing this
	// forces a new resource.
	Name string `pulumi:"name"`

	// Environment is the Modal environment the app lives in. Changing this
	// forces a new resource. Falls back to the provider-level environment.
	Environment *string `pulumi:"environment,optional"`

	// Tags are key/value tags set on the app. Updated in place.
	Tags map[string]string `pulumi:"tags,optional"`
}

// AppState holds the computed outputs for a Modal App.
type AppState struct {
	AppArgs

	// AppId is the Modal-assigned unique identifier for the app.
	AppId string `pulumi:"appId"`
}

func (r *App) Annotate(a infer.Annotator) {
	a.Describe(&r, "A named Modal App, the container that Functions, Images and Sandboxes belong to. "+
		"The resource creates the empty named App; it does not deploy code. Deleting it stops the App. "+
		"See https://modal.com/docs/guide/apps.")
}

func (a *AppArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The app's unique name within its environment. Changing this forces a new resource.")
	ann.Describe(&a.Environment, "The Modal environment the app lives in. Changing this forces a new resource. "+
		"When unset, the provider-level environment or the account's default environment is used.")
	ann.Describe(&a.Tags, "Key/value tags set on the app. Updated in place.")
}

func (s *AppState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.AppId, "The Modal-assigned identifier of the App.")
}

var _ infer.CustomResource[AppArgs, AppState] = (*App)(nil)
var _ infer.CustomDiff[AppArgs, AppState] = (*App)(nil)
var _ infer.CustomUpdate[AppArgs, AppState] = (*App)(nil)
var _ infer.CustomRead[AppArgs, AppState] = (*App)(nil)
var _ infer.CustomDelete[AppState] = (*App)(nil)

// Diff replaces on a name or environment change (Modal has no rename for
// apps) and updates tags in place.
func (r *App) Diff(ctx context.Context, req infer.DiffRequest[AppArgs, AppState]) (infer.DiffResponse, error) {
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
	if !maps.Equal(req.Inputs.Tags, req.State.Tags) && (len(req.Inputs.Tags) > 0 || len(req.State.Tags) > 0) {
		resp.DetailedDiff["tags"] = pgp.PropertyDiff{Kind: pgp.Update}
		resp.HasChanges = true
	}
	return resp, nil
}

func (r *App) Create(ctx context.Context, req infer.CreateRequest[AppArgs]) (infer.CreateResponse[AppState], error) {
	args := req.Inputs
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		state := AppState{AppArgs: args}
		if env := resolveEnv(args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret)); env != "" {
			state.Environment = &env
		}
		return infer.CreateResponse[AppState]{ID: "", Output: state}, nil
	}
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[AppState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	environment := effectiveEnvironment(rpcCtx, client, args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret))
	// Modal Apps have no fail-if-exists creation type: the live API rejects
	// CREATE_FAIL_IF_EXISTS for Apps with "Invalid object creation type". Look
	// the name up first so a duplicate still fails, then create-if-missing like
	// the official SDK does.
	if existing, found, err := lookupAppID(rpcCtx, client, args.Name, environment); err != nil {
		return infer.CreateResponse[AppState]{}, fmt.Errorf("modal: create app %q: %w", args.Name, err)
	} else if found {
		return infer.CreateResponse[AppState]{}, fmt.Errorf("modal: create app %q: an app with this name already exists in %s (%s); import it instead", args.Name, environmentLabel(environment), existing)
	}
	resp, err := client.stub.AppGetOrCreate(rpcCtx, pb.AppGetOrCreateRequest_builder{
		AppName:            args.Name,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_IF_MISSING,
	}.Build())
	if err != nil {
		return infer.CreateResponse[AppState]{}, fmt.Errorf("modal: create app %q: %w", args.Name, err)
	}
	id := resp.GetAppId()
	if len(args.Tags) > 0 {
		if _, err := client.stub.AppSetTags(rpcCtx, pb.AppSetTagsRequest_builder{AppId: id, Tags: args.Tags}.Build()); err != nil {
			// The App exists without its tags. Record it as partial state without
			// them, so the next Diff sees a tags change and Update retries.
			partial := AppState{AppArgs: args, AppId: id}
			partial.Tags = nil
			if environment != "" {
				partial.Environment = &environment
			}
			return infer.CreateResponse[AppState]{ID: id, Output: partial},
				infer.ResourceInitFailedError{Reasons: []string{fmt.Sprintf("set tags on app %q: %v", args.Name, err)}}
		}
	}
	state := AppState{AppArgs: args, AppId: id}
	if environment != "" {
		state.Environment = &environment
	}
	return infer.CreateResponse[AppState]{ID: id, Output: state}, nil
}

// lookupApp resolves an App by name. The live API answers a lookup of a
// missing App with success and an empty ID rather than NotFound, so both are
// reported as (nil, nil).
func lookupApp(ctx context.Context, client *modalClient, name, environment string) (*pb.AppGetByDeploymentNameResponse, error) {
	got, err := client.stub.AppGetByDeploymentName(ctx, pb.AppGetByDeploymentNameRequest_builder{
		Name: name, EnvironmentName: environment,
	}.Build())
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if got.GetAppId() == "" {
		return nil, nil
	}
	return got, nil
}

// lookupAppID is lookupApp reduced to the App's ID.
func lookupAppID(ctx context.Context, client *modalClient, name, environment string) (string, bool, error) {
	got, err := lookupApp(ctx, client, name, environment)
	if err != nil || got == nil {
		return "", false, err
	}
	return got.GetAppId(), true, nil
}

func environmentLabel(environment string) string {
	if environment == "" {
		return "the default environment"
	}
	return fmt.Sprintf("environment %q", environment)
}

func appStopped(info *pb.AppHandleMetadata) bool {
	switch info.GetLifecycle().GetAppState() {
	case pb.AppState_APP_STATE_STOPPED, pb.AppState_APP_STATE_STOPPING:
		return true
	}
	return false
}

// Read refreshes the app by ID: environment, name (via the environment's app
// list) and tags. A stopped App is reported as gone.
func (r *App) Read(ctx context.Context, req infer.ReadRequest[AppArgs, AppState]) (infer.ReadResponse[AppArgs, AppState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[AppArgs, AppState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	info, err := client.stub.AppGetInfo(rpcCtx, pb.AppGetInfoRequest_builder{AppId: req.ID}.Build())
	if isNotFound(err) {
		if err := notFoundOnImport("app", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("modal: read app %q: %w", req.ID, err)
	}
	if appStopped(info.GetInfo()) {
		if err := notFoundOnImport("app", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}
	environment := info.GetInfo().GetEnvironmentName()

	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	state.AppId = req.ID
	if environment != "" {
		state.Environment = &environment
	}

	// The app name is not part of the handle metadata; the environment's app
	// list carries it.
	list, err := client.stub.AppList(rpcCtx, pb.AppListRequest_builder{EnvironmentName: environment}.Build())
	if err != nil {
		return empty, fmt.Errorf("modal: list apps to read %q: %w", req.ID, err)
	}
	found := false
	for _, item := range list.GetApps() {
		if item.GetAppId() == req.ID {
			state.Name = item.GetName()
			found = true
			break
		}
	}
	if !found {
		if err := notFoundOnImport("app", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}

	tags, err := client.stub.AppGetTags(rpcCtx, pb.AppGetTagsRequest_builder{AppId: req.ID}.Build())
	if err != nil {
		return empty, fmt.Errorf("modal: read tags of app %q: %w", req.ID, err)
	}
	if len(tags.GetTags()) > 0 {
		state.Tags = tags.GetTags()
	} else {
		state.Tags = nil
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = state.Name
	inputs.Tags = state.Tags
	if importing && environment != "" {
		inputs.Environment = &environment
	}
	return infer.ReadResponse[AppArgs, AppState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

func (r *App) Update(ctx context.Context, req infer.UpdateRequest[AppArgs, AppState]) (infer.UpdateResponse[AppState], error) {
	args := req.Inputs
	state := req.State
	state.AppArgs = args
	state.Environment = req.State.Environment
	if req.DryRun {
		return infer.UpdateResponse[AppState]{Output: state}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[AppState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	tags := args.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	if _, err := client.stub.AppSetTags(rpcCtx, pb.AppSetTagsRequest_builder{AppId: req.ID, Tags: tags}.Build()); err != nil {
		return infer.UpdateResponse[AppState]{}, fmt.Errorf("modal: set tags on app %q: %w", req.State.Name, err)
	}
	return infer.UpdateResponse[AppState]{Output: state}, nil
}

// Delete stops the App. Modal retires stopped Apps, so they no longer appear
// in listings and their name becomes free.
func (r *App) Delete(ctx context.Context, req infer.DeleteRequest[AppState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.AppStop(rpcCtx, pb.AppStopRequest_builder{
		AppId:  req.ID,
		Source: pb.AppStopSource_APP_STOP_SOURCE_PYTHON_CLIENT,
	}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: stop app %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
