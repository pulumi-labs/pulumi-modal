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

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// This file extends the fake Modal control plane with the Dict, Environment,
// App and Image services and the lookup RPCs behind the data sources.

type fakeEnv struct {
	id        string
	name      string
	isDefault bool
	suffix    string
	maxTasks  *int32
	maxGpus   *int32
}

type fakeApp struct {
	id      string
	name    string
	env     string
	tags    map[string]string
	stopped bool
}

type fakeImage struct {
	id       string
	env      string
	commands []string
	baseTag  string
	secrets  []string
	gpu      bool
	builds   int
}

// --- Environment ---------------------------------------------------------------

func (f *fakeModal) envItem(e *fakeEnv) *pb.EnvironmentListItem {
	return pb.EnvironmentListItem_builder{
		Name: e.name, EnvironmentId: e.id, Default: e.isDefault, WebhookSuffix: e.suffix,
		MaxConcurrentTasks: e.maxTasks, MaxConcurrentGpus: e.maxGpus,
	}.Build()
}

func (f *fakeModal) EnvironmentList(context.Context, *emptypb.Empty) (*pb.EnvironmentListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.envListed++
	var items []*pb.EnvironmentListItem
	for _, e := range f.envs {
		items = append(items, f.envItem(e))
	}
	return pb.EnvironmentListResponse_builder{Items: items}.Build(), nil
}

func (f *fakeModal) envByName(name string) *fakeEnv {
	for _, e := range f.envs {
		if e.name == name || (name == "" && e.isDefault) {
			return e
		}
	}
	return nil
}

func (f *fakeModal) EnvironmentGetOrCreate(_ context.Context, req *pb.EnvironmentGetOrCreateRequest) (*pb.EnvironmentGetOrCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.envByName(req.GetDeploymentName())
	if e == nil {
		return nil, status.Error(codes.NotFound, "environment not found")
	}
	return pb.EnvironmentGetOrCreateResponse_builder{EnvironmentId: e.id, Metadata: pb.EnvironmentMetadata_builder{
		Name: e.name, Settings: pb.EnvironmentSettings_builder{ImageBuilderVersion: "2024.10", WebhookSuffix: e.suffix}.Build(),
	}.Build()}.Build(), nil
}

func (f *fakeModal) EnvironmentCreate(_ context.Context, req *pb.EnvironmentCreateRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.envByName(req.GetName()) != nil {
		return nil, status.Error(codes.AlreadyExists, "environment exists")
	}
	f.seq++
	id := "env-" + req.GetName()
	e := &fakeEnv{id: id, name: req.GetName(), suffix: req.GetSettings().GetWebhookSuffix()}
	if e.suffix == "" {
		e.suffix = "auto-" + req.GetName()
	}
	if req.GetSettings().HasMaxConcurrentTasks() {
		v := req.GetSettings().GetMaxConcurrentTasks()
		e.maxTasks = &v
	}
	if req.GetSettings().HasMaxConcurrentGpus() {
		v := req.GetSettings().GetMaxConcurrentGpus()
		e.maxGpus = &v
	}
	f.envs[id] = e
	return &emptypb.Empty{}, nil
}

func (f *fakeModal) EnvironmentUpdate(_ context.Context, req *pb.EnvironmentUpdateRequest) (*pb.EnvironmentListItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.envByName(req.GetCurrentName())
	if e == nil {
		return nil, status.Error(codes.NotFound, "environment not found")
	}
	if req.GetName() != nil {
		e.name = req.GetName().GetValue()
	}
	if req.GetWebSuffix() != nil {
		e.suffix = req.GetWebSuffix().GetValue()
	}
	if req.HasMaxConcurrentTasks() {
		v := req.GetMaxConcurrentTasks()
		e.maxTasks = &v
	}
	if req.HasMaxConcurrentGpus() {
		v := req.GetMaxConcurrentGpus()
		e.maxGpus = &v
	}
	// Like the live API, the update response is sparse: no name, no ID.
	return pb.EnvironmentListItem_builder{WebhookSuffix: e.suffix}.Build(), nil
}

func (f *fakeModal) EnvironmentDelete(_ context.Context, req *pb.EnvironmentDeleteRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.envByName(req.GetName())
	if e == nil {
		return nil, status.Error(codes.NotFound, "environment not found")
	}
	delete(f.envs, e.id)
	return &emptypb.Empty{}, nil
}

// --- Dict ----------------------------------------------------------------------

func (f *fakeModal) DictGetOrCreate(_ context.Context, req *pb.DictGetOrCreateRequest) (*pb.DictGetOrCreateResponse, error) {
	o, err := f.getOrCreate(f.dicts, "di", req.GetDeploymentName(), req.GetEnvironmentName(), req.GetObjectCreationType(), nil)
	if err != nil {
		return nil, err
	}
	return pb.DictGetOrCreateResponse_builder{DictId: o.id}.Build(), nil
}

func (f *fakeModal) DictGetById(_ context.Context, req *pb.DictGetByIdRequest) (*pb.DictGetByIdResponse, error) {
	o, err := f.get(f.dicts, req.GetDictId())
	if err != nil {
		return nil, err
	}
	return pb.DictGetByIdResponse_builder{DictId: o.id, Metadata: pb.DictMetadata_builder{Name: o.name}.Build()}.Build(), nil
}

func (f *fakeModal) DictDelete(_ context.Context, req *pb.DictDeleteRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, f.remove(f.dicts, req.GetDictId())
}

// --- App -----------------------------------------------------------------------

func (f *fakeModal) AppGetOrCreate(_ context.Context, req *pb.AppGetOrCreateRequest) (*pb.AppGetOrCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env := f.resolveEnv(req.GetEnvironmentName())
	// Like the live API, Apps do not support CREATE_FAIL_IF_EXISTS.
	if req.GetObjectCreationType() == pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS {
		return nil, status.Error(codes.InvalidArgument, "Invalid object creation type")
	}
	for _, a := range f.apps {
		if a.name == req.GetAppName() && a.env == env && !a.stopped {
			return pb.AppGetOrCreateResponse_builder{AppId: a.id}.Build(), nil
		}
	}
	if req.GetObjectCreationType() == pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.NotFound, "app not found")
	}
	a := &fakeApp{id: f.nextID("ap"), name: req.GetAppName(), env: env, tags: map[string]string{}}
	f.apps[a.id] = a
	return pb.AppGetOrCreateResponse_builder{AppId: a.id}.Build(), nil
}

func (f *fakeModal) AppGetInfo(_ context.Context, req *pb.AppGetInfoRequest) (*pb.AppGetInfoResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.apps[req.GetAppId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "app not found")
	}
	state := pb.AppState_APP_STATE_EPHEMERAL
	if a.stopped {
		state = pb.AppState_APP_STATE_STOPPED
	}
	return pb.AppGetInfoResponse_builder{Info: pb.AppHandleMetadata_builder{
		AppId: a.id, EnvironmentName: a.env, Lifecycle: pb.AppLifecycle_builder{AppState: state}.Build(),
	}.Build()}.Build(), nil
}

func (f *fakeModal) AppList(_ context.Context, req *pb.AppListRequest) (*pb.AppListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env := f.resolveEnv(req.GetEnvironmentName())
	var items []*pb.AppListResponse_AppListItem
	for _, a := range f.apps {
		if a.env == env && !a.stopped {
			items = append(items, pb.AppListResponse_AppListItem_builder{AppId: a.id, Name: a.name}.Build())
		}
	}
	return pb.AppListResponse_builder{Apps: items}.Build(), nil
}

func (f *fakeModal) AppGetTags(_ context.Context, req *pb.AppGetTagsRequest) (*pb.AppGetTagsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.apps[req.GetAppId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "app not found")
	}
	return pb.AppGetTagsResponse_builder{Tags: a.tags}.Build(), nil
}

func (f *fakeModal) AppSetTags(_ context.Context, req *pb.AppSetTagsRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSetTags {
		return nil, status.Error(codes.Unavailable, "tags unavailable")
	}
	a, ok := f.apps[req.GetAppId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "app not found")
	}
	a.tags = map[string]string{}
	for k, v := range req.GetTags() {
		a.tags[k] = v
	}
	return &emptypb.Empty{}, nil
}

func (f *fakeModal) AppStop(_ context.Context, req *pb.AppStopRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.apps[req.GetAppId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "app not found")
	}
	a.stopped = true
	return &emptypb.Empty{}, nil
}

func (f *fakeModal) AppGetByDeploymentName(_ context.Context, req *pb.AppGetByDeploymentNameRequest) (*pb.AppGetByDeploymentNameResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env := f.resolveEnv(req.GetEnvironmentName())
	for _, a := range f.apps {
		if a.name == req.GetName() && a.env == env && !a.stopped {
			return pb.AppGetByDeploymentNameResponse_builder{AppId: a.id, EnvironmentName: a.env}.Build(), nil
		}
	}
	// Like the live API: a missing App is a success with an empty ID.
	return pb.AppGetByDeploymentNameResponse_builder{}.Build(), nil
}

// --- Image ---------------------------------------------------------------------

func (f *fakeModal) ImageGetOrCreate(_ context.Context, req *pb.ImageGetOrCreateRequest) (*pb.ImageGetOrCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	app, ok := f.apps[req.GetAppId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "build app not found")
	}
	img := &fakeImage{id: f.nextID("im"), env: app.env, secrets: req.GetImage().GetSecretIds(), gpu: req.GetImage().HasGpuConfig()}
	for _, c := range req.GetImage().GetDockerfileCommands() {
		img.commands = append(img.commands, c)
	}
	if len(img.commands) > 0 {
		img.baseTag = img.commands[0]
	}
	f.images[img.id] = img
	return pb.ImageGetOrCreateResponse_builder{
		ImageId: img.id,
		Result:  pb.GenericResult_builder{Status: pb.GenericResult_GENERIC_STATUS_SUCCESS}.Build(),
	}.Build(), nil
}

func (f *fakeModal) ImageFromId(_ context.Context, req *pb.ImageFromIdRequest) (*pb.ImageFromIdResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.images[req.GetImageId()]; !ok {
		return nil, status.Error(codes.NotFound, "image not found")
	}
	return pb.ImageFromIdResponse_builder{ImageId: req.GetImageId()}.Build(), nil
}

func (f *fakeModal) tagKey(env, tag string) string { return f.resolveEnv(env) + "/" + tag }

func (f *fakeModal) ImagePublish(_ context.Context, req *pb.ImagePublishRequest) (*pb.ImagePublishResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failPublish {
		return nil, status.Error(codes.Unavailable, "publish unavailable")
	}
	if _, ok := f.images[req.GetImageId()]; !ok {
		return nil, status.Error(codes.NotFound, "image not found")
	}
	f.tags[f.tagKey(req.GetEnvironmentName(), req.GetTag())] = req.GetImageId()
	return pb.ImagePublishResponse_builder{}.Build(), nil
}

func (f *fakeModal) ImageGetByTag(_ context.Context, req *pb.ImageGetByTagRequest) (*pb.ImageGetByTagResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.tags[f.tagKey(req.GetEnvironmentName(), req.GetTag())]
	if !ok {
		return nil, status.Error(codes.NotFound, "tag not found")
	}
	return pb.ImageGetByTagResponse_builder{ImageId: id}.Build(), nil
}

func (f *fakeModal) ImageListTags(_ context.Context, req *pb.ImageListTagsRequest) (*pb.ImageListTagsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := f.resolveEnv(req.GetEnvironmentName()) + "/"
	var items []*pb.ImageListTagsItem
	for k, id := range f.tags {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix && (req.GetImageId() == "" || req.GetImageId() == id) {
			items = append(items, pb.ImageListTagsItem_builder{Tag: k[len(prefix):], ImageId: id}.Build())
		}
	}
	return pb.ImageListTagsResponse_builder{Items: items}.Build(), nil
}

func (f *fakeModal) ImageNameDelete(_ context.Context, req *pb.ImageNameDeleteRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.tags {
		if len(k) > len(req.GetTag()) && k[len(k)-len(req.GetTag()):] == req.GetTag() {
			delete(f.tags, k)
			return &emptypb.Empty{}, nil
		}
	}
	return nil, status.Error(codes.NotFound, "tag not found")
}

func (f *fakeModal) ImageDelete(_ context.Context, req *pb.ImageDeleteRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.images[req.GetImageId()]; !ok {
		return nil, status.Error(codes.NotFound, "image not found")
	}
	delete(f.images, req.GetImageId())
	return &emptypb.Empty{}, nil
}

// --- Lookups -------------------------------------------------------------------

func (f *fakeModal) FunctionGet(_ context.Context, req *pb.FunctionGetRequest) (*pb.FunctionGetResponse, error) {
	if req.GetAppName() != "deployed-app" || req.GetObjectTag() != "predict" {
		return nil, status.Error(codes.NotFound, "function not found")
	}
	return pb.FunctionGetResponse_builder{FunctionId: "fu-1", HandleMetadata: pb.FunctionHandleMetadata_builder{WebUrl: "https://example--predict.modal.run"}.Build()}.Build(), nil
}

func (f *fakeModal) ClassGet(_ context.Context, req *pb.ClassGetRequest) (*pb.ClassGetResponse, error) {
	if req.GetAppName() != "deployed-app" || req.GetObjectTag() != "Model" {
		return nil, status.Error(codes.NotFound, "class not found")
	}
	return pb.ClassGetResponse_builder{ClassId: "cs-1", HandleMetadata: pb.ClassHandleMetadata_builder{Methods: []*pb.ClassMethod{
		pb.ClassMethod_builder{FunctionName: "train"}.Build(), pb.ClassMethod_builder{FunctionName: "infer"}.Build(),
	}}.Build()}.Build(), nil
}

func (f *fakeModal) ProxyGet(_ context.Context, req *pb.ProxyGetRequest) (*pb.ProxyGetResponse, error) {
	if req.GetName() != "egress" {
		return nil, status.Error(codes.NotFound, "proxy not found")
	}
	return pb.ProxyGetResponse_builder{Proxy: pb.Proxy_builder{
		ProxyId: "pr-1", Name: "egress", Region: "us-east", ProxyIps: []*pb.ProxyIp{
			pb.ProxyIp_builder{ProxyIp: "203.0.113.2"}.Build(), pb.ProxyIp_builder{ProxyIp: "203.0.113.1"}.Build(),
		},
	}.Build()}.Build(), nil
}

func (f *fakeModal) TokenInfoGet(context.Context, *pb.TokenInfoGetRequest) (*pb.TokenInfoGetResponse, error) {
	return pb.TokenInfoGetResponse_builder{TokenId: "ak-test", TokenName: "ci", WorkspaceId: "ac-1", WorkspaceName: "acme"}.Build(), nil
}

func (f *fakeModal) WorkspaceSettings(context.Context, *emptypb.Empty) (*pb.WorkspaceSettingsResponse, error) {
	return pb.WorkspaceSettingsResponse_builder{DefaultEnvironmentName: "main", ImageBuilderVersion: "2024.10"}.Build(), nil
}
