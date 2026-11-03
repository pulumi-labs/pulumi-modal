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
	"strings"
	"time"

	modal "github.com/modal-labs/modal-client/go"
	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ---------------------------------------------------------------------------
// Image resource
// ---------------------------------------------------------------------------

// buildAppName is the App Modal requires an Image build to belong to. The
// provider creates it on demand in the Image's environment.
const buildAppName = "pulumi-image-builds"

// imageBuildTimeout bounds a whole build (base pull plus every layer).
const imageBuildTimeout = 45 * time.Minute

// Image builds a Modal Image from a registry base image and optional
// Dockerfile commands, and can publish it under a stable name. Modal caches
// layers, so rebuilding an unchanged image is fast.
type Image struct{}

// ImageArgs are the user-supplied inputs for a Modal Image.
type ImageArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// BaseImage is the registry image to start from, for example
	// "python:3.12-slim". Required when creating an Image.
	BaseImage *string `pulumi:"baseImage,optional"`

	// DockerfileCommands are Dockerfile instructions applied on top of the
	// base image, in one layer (for example RUN pip install ...).
	DockerfileCommands []string `pulumi:"dockerfileCommands,optional"`

	// Env sets environment variables in the build environment of the
	// Dockerfile commands layer.
	Env map[string]string `pulumi:"env,optional"`

	// Gpu reserves a GPU for the build of the Dockerfile commands layer,
	// for example "T4" or "A100:2".
	Gpu *string `pulumi:"gpu,optional"`

	// RegistrySecret is the name of a Modal Secret holding the credentials
	// for a private registry the base image is pulled from.
	RegistrySecret *string `pulumi:"registrySecret,optional"`

	// Name publishes the built Image under a stable name, "name" or
	// "name:tag" (the tag defaults to latest). Changing it republishes in
	// place.
	Name *string `pulumi:"name,optional"`

	// Environment is the Modal environment the Image is built and published
	// in. Changing this forces a new resource. Falls back to the
	// provider-level environment.
	Environment *string `pulumi:"environment,optional"`
}

// ImageState holds the computed outputs for a Modal Image.
type ImageState struct {
	ImageArgs

	// ImageId is the Modal-assigned unique identifier for the built image.
	ImageId string `pulumi:"imageId"`
}

func (r *Image) Annotate(a infer.Annotator) {
	a.Describe(&r, "A Modal Image built from a registry base image plus optional Dockerfile commands, optionally "+
		"published under a stable name. Changing the base image, commands, environment variables, GPU, registry secret "+
		"or environment builds a new Image and replaces this one. An imported Image keeps whatever it was built from. "+
		"See https://modal.com/docs/guide/images.")
}

func (a *ImageArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.BaseImage, "Registry image to start from, for example \"python:3.12-slim\". Required when creating an Image. "+
		"Changing this builds a new Image.")
	ann.Describe(&a.DockerfileCommands, "Dockerfile instructions applied on top of the base image in a single layer, "+
		"for example \"RUN pip install torch\". Changing this builds a new Image.")
	ann.Describe(&a.Env, "Environment variables set in the build environment of the Dockerfile commands layer. "+
		"Changing this builds a new Image.")
	ann.Describe(&a.Gpu, "GPU reserved for the build of the Dockerfile commands layer, for example \"T4\" or \"A100:2\". "+
		"Changing this builds a new Image.")
	ann.Describe(&a.RegistrySecret, "Name of a Modal Secret holding credentials for a private registry. Changing this builds a new Image.")
	ann.Describe(&a.Name, "Publishes the built Image under a stable name, \"name\" or \"name:tag\" (tag defaults to latest). "+
		"Changing this republishes the same Image under the new name.")
	ann.Describe(&a.Environment, "The Modal environment the Image is built and published in. Changing this forces a new resource. "+
		"When unset, the provider-level environment or the account's default environment is used.")
}

func (s *ImageState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.ImageId, "The Modal-assigned identifier of the built Image.")
}

var _ infer.CustomResource[ImageArgs, ImageState] = (*Image)(nil)
var _ infer.CustomCheck[ImageArgs] = (*Image)(nil)
var _ infer.CustomDiff[ImageArgs, ImageState] = (*Image)(nil)
var _ infer.CustomUpdate[ImageArgs, ImageState] = (*Image)(nil)
var _ infer.CustomRead[ImageArgs, ImageState] = (*Image)(nil)
var _ infer.CustomDelete[ImageState] = (*Image)(nil)

// imageTag normalises a published name to "name:tag".
func imageTag(name string) string {
	if strings.Contains(name, ":") {
		return name
	}
	return name + ":latest"
}

// namedTag is the normalised published tag of an optional name ("" when unset),
// so "foo" and "foo:latest" compare equal everywhere.
func namedTag(name *string) string {
	if name == nil || *name == "" {
		return ""
	}
	return imageTag(*name)
}

// Check rejects names the build SDK would reject, at preview time.
func (r *Image) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[ImageArgs], error) {
	args, failures, err := infer.DefaultCheck[ImageArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[ImageArgs]{}, err
	}
	if args.Name != nil {
		name := *args.Name
		switch {
		case name == "" || strings.HasPrefix(name, ":"):
			failures = append(failures, pgp.CheckFailure{Property: "name", Reason: "image name must not be empty"})
		case strings.Contains(name, "/"):
			failures = append(failures, pgp.CheckFailure{Property: "name",
				Reason: "image name must not contain '/'; set the environment property to publish into another environment"})
		case strings.HasPrefix(name, "im-"):
			failures = append(failures, pgp.CheckFailure{Property: "name",
				Reason: "image name cannot start with 'im-' (reserved for image IDs)"})
		}
	}
	return infer.CheckResponse[ImageArgs]{Inputs: args, Failures: failures}, nil
}

// Diff replaces on any build input change and updates the published name in
// place. An imported Image (state without a base image) adopts the program
// as is: Modal does not store what an image was built from.
func (r *Image) Diff(ctx context.Context, req infer.DiffRequest[ImageArgs, ImageState]) (infer.DiffResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	replace := func(key string) {
		resp.DetailedDiff[key] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
		resp.HasChanges = true
	}
	if environmentChanged(req.Inputs.Environment, req.State.Environment, cfg) {
		replace("environment")
	}
	imported := req.State.BaseImage == nil
	if !imported {
		if derefStr(req.Inputs.BaseImage) != derefStr(req.State.BaseImage) {
			replace("baseImage")
		}
		if !slices.Equal(req.Inputs.DockerfileCommands, req.State.DockerfileCommands) {
			replace("dockerfileCommands")
		}
		if len(req.Inputs.Env) != len(req.State.Env) || !mapsEqual(req.Inputs.Env, req.State.Env) {
			replace("env")
		}
		if derefStr(req.Inputs.Gpu) != derefStr(req.State.Gpu) {
			replace("gpu")
		}
		if derefStr(req.Inputs.RegistrySecret) != derefStr(req.State.RegistrySecret) {
			replace("registrySecret")
		}
	}
	if namedTag(req.Inputs.Name) != namedTag(req.State.Name) {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.Update}
		resp.HasChanges = true
	}
	return resp, nil
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func (r *Image) Create(ctx context.Context, req infer.CreateRequest[ImageArgs]) (infer.CreateResponse[ImageState], error) {
	args := req.Inputs
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		state := ImageState{ImageArgs: args}
		if env := resolveStr(args.Environment, cfg.Environment); env != "" {
			state.Environment = &env
		}
		return infer.CreateResponse[ImageState]{ID: derefStr(args.BaseImage), Output: state}, nil
	}
	if derefStr(args.BaseImage) == "" {
		return infer.CreateResponse[ImageState]{}, fmt.Errorf("modal: baseImage is required to build an Image")
	}

	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[ImageState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	buildCtx, cancel := context.WithTimeout(ctx, imageBuildTimeout)
	defer cancel()
	environment := effectiveEnvironment(buildCtx, client, args.Environment, cfg)

	app, err := client.stub.AppGetOrCreate(buildCtx, pb.AppGetOrCreateRequest_builder{
		AppName:            buildAppName,
		EnvironmentName:    environment,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_IF_MISSING,
	}.Build())
	if err != nil {
		return infer.CreateResponse[ImageState]{}, fmt.Errorf("modal: get build app: %w", err)
	}

	sdk, err := modal.NewClientWithOptions(&modal.ClientParams{
		TokenID:            resolveStr(args.TokenID, cfg.TokenID),
		TokenSecret:        resolveStr(args.TokenSecret, cfg.TokenSecret),
		Environment:        environment,
		ControlPlaneClient: client.stub,
	})
	if err != nil {
		return infer.CreateResponse[ImageState]{}, fmt.Errorf("modal: build client: %w", err)
	}

	var pull *modal.ImageFromRegistryParams
	if name := derefStr(args.RegistrySecret); name != "" {
		secret, err := sdk.Secrets.FromName(buildCtx, name, &modal.SecretFromNameParams{Environment: environment})
		if err != nil {
			return infer.CreateResponse[ImageState]{}, fmt.Errorf("modal: registry secret %q: %w", name, err)
		}
		pull = &modal.ImageFromRegistryParams{Secret: secret}
	}
	img := sdk.Images.FromRegistry(*args.BaseImage, pull)
	if len(args.DockerfileCommands) > 0 {
		img = img.DockerfileCommands(args.DockerfileCommands, &modal.ImageDockerfileCommandsParams{
			Env: args.Env,
			GPU: derefStr(args.Gpu),
		})
	}
	built, err := img.Build(buildCtx, &modal.App{AppID: app.GetAppId(), Name: buildAppName, Environment: environment}, nil)
	if err != nil {
		return infer.CreateResponse[ImageState]{}, fmt.Errorf("modal: build image from %q: %w", *args.BaseImage, err)
	}

	state := ImageState{ImageArgs: args, ImageId: built.ImageID}
	if environment != "" {
		state.Environment = &environment
	}
	if name := derefStr(args.Name); name != "" {
		if err := built.Publish(buildCtx, name, &modal.ImagePublishParams{Environment: environment}); err != nil {
			// The image exists but is not published. Record it without the name
			// as partial state, so the next Diff sees the name as a change and
			// Update retries the publish instead of reporting no changes.
			partial := state
			partial.Name = nil
			return infer.CreateResponse[ImageState]{ID: built.ImageID, Output: partial},
				infer.ResourceInitFailedError{Reasons: []string{fmt.Sprintf("publish image as %q: %v", name, err)}}
		}
	}
	return infer.CreateResponse[ImageState]{ID: built.ImageID, Output: state}, nil
}

// Read refreshes the image by ID. For a published image it also checks that
// the name still points at this image; if it does not, the name is dropped
// from state so the next update republishes it. Modal does not report what an
// image was built from, so an imported image has no build inputs.
func (r *Image) Read(ctx context.Context, req infer.ReadRequest[ImageArgs, ImageState]) (infer.ReadResponse[ImageArgs, ImageState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[ImageArgs, ImageState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	if _, err := client.stub.ImageFromId(rpcCtx, pb.ImageFromIdRequest_builder{ImageId: req.ID}.Build()); isNotFound(err) {
		return empty, nil
	} else if err != nil {
		return empty, fmt.Errorf("modal: read image %q: %w", req.ID, err)
	}

	importing := req.State.Name == nil && req.State.BaseImage == nil && req.Inputs.BaseImage == nil && req.Inputs.Name == nil
	state := req.State
	state.ImageId = req.ID
	environment := derefStr(state.Environment)
	if environment == "" {
		environment = effectiveEnvironment(rpcCtx, client, nil, cfg)
	}

	// Published names pointing at this image.
	tags, err := client.stub.ImageListTags(rpcCtx, pb.ImageListTagsRequest_builder{
		EnvironmentName: environment,
		ImageId:         req.ID,
	}.Build())
	if err != nil {
		return empty, fmt.Errorf("modal: list tags of image %q: %w", req.ID, err)
	}
	var published []string
	for _, item := range tags.GetItems() {
		published = append(published, item.GetTag())
	}
	if want := derefStr(state.Name); want != "" {
		if !slices.Contains(published, imageTag(want)) {
			state.Name = nil
		}
	} else if importing && len(published) > 0 {
		name := published[0]
		state.Name = &name
	}
	if environment != "" {
		state.Environment = &environment
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	if importing {
		inputs.Name = state.Name
		if environment != "" {
			inputs.Environment = &environment
		}
	} else if inputs.Name != nil {
		inputs.Name = state.Name
	}
	return infer.ReadResponse[ImageArgs, ImageState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

// Update republishes the image under a new name and removes the old name.
func (r *Image) Update(ctx context.Context, req infer.UpdateRequest[ImageArgs, ImageState]) (infer.UpdateResponse[ImageState], error) {
	args := req.Inputs
	state := req.State
	if args.Name != nil || req.State.Name != nil {
		state.Name = args.Name
	}
	state.TokenID, state.TokenSecret = args.TokenID, args.TokenSecret
	// "foo" and "foo:latest" are the same published name: nothing to republish.
	if req.DryRun || namedTag(args.Name) == namedTag(req.State.Name) {
		return infer.UpdateResponse[ImageState]{Output: state}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[ImageState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	environment := derefStr(req.State.Environment)
	if name := derefStr(args.Name); name != "" {
		if _, err := client.stub.ImagePublish(rpcCtx, pb.ImagePublishRequest_builder{
			ImageId:         req.ID,
			Tag:             imageTag(name),
			EnvironmentName: environment,
		}.Build()); err != nil {
			return infer.UpdateResponse[ImageState]{}, fmt.Errorf("modal: publish image as %q: %w", name, err)
		}
	}
	if old := derefStr(req.State.Name); old != "" {
		if err := deleteImageName(rpcCtx, client, req.ID, imageTag(old), environment); err != nil {
			return infer.UpdateResponse[ImageState]{}, err
		}
	}
	return infer.UpdateResponse[ImageState]{Output: state}, nil
}

// deleteImageName removes a published name, but only while it still points at
// imageID: a replacement may already have taken the name over.
func deleteImageName(ctx context.Context, client *modalClient, imageID, tag, environment string) error {
	cur, err := client.stub.ImageGetByTag(ctx, pb.ImageGetByTagRequest_builder{Tag: tag, EnvironmentName: environment}.Build())
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("modal: look up image name %q: %w", tag, err)
	}
	if cur.GetImageId() != imageID {
		return nil
	}
	if _, err := client.stub.ImageNameDelete(ctx, pb.ImageNameDeleteRequest_builder{Tag: tag}.Build()); err != nil && !isNotFound(err) {
		return fmt.Errorf("modal: delete image name %q: %w", tag, err)
	}
	return nil
}

// Delete removes the published name (if it still points here) and the image.
func (r *Image) Delete(ctx context.Context, req infer.DeleteRequest[ImageState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	if name := derefStr(req.State.Name); name != "" {
		if err := deleteImageName(rpcCtx, client, req.ID, imageTag(name), derefStr(req.State.Environment)); err != nil {
			return infer.DeleteResponse{}, err
		}
	}
	if _, err := client.stub.ImageDelete(rpcCtx, pb.ImageDeleteRequest_builder{ImageId: req.ID}.Build()); err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete image %q: %w", req.ID, err)
	}
	return infer.DeleteResponse{}, nil
}
