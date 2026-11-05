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
	"github.com/pulumi/pulumi-go-provider/infer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------------
// Lookup functions
// ---------------------------------------------------------------------------
//
// Every lookup resolves an existing object by name (and environment) with the
// provider's credentials and never creates anything: a missing object is an
// error that names the object and environment.

// lookupSession opens a client for a lookup and resolves its environment.
func lookupSession(ctx context.Context, environment *string) (*modalClient, string, func(), error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, nil, nil)
	if err != nil {
		return nil, "", nil, fmt.Errorf("modal: %w", err)
	}
	return client, resolveEnv(environment, cfg), func() { _ = client.Close() }, nil
}

// lookupEnvironment is the environment to report for a looked-up object: the one
// that was searched, else the account default (the lookup request itself leaves
// an unset environment to the server).
func lookupEnvironment(ctx context.Context, client *modalClient, env string) string {
	if env != "" {
		return env
	}
	return effectiveEnvironment(ctx, client, nil, ProviderConfig{})
}

func lookupErr(kind, name, environment string, err error) error {
	where := "the default environment"
	if environment != "" {
		where = fmt.Sprintf("environment %q", environment)
	}
	if isNotFound(err) {
		return fmt.Errorf("modal: %s %q was not found in %s", kind, name, where)
	}
	return fmt.Errorf("modal: look up %s %q in %s: %w", kind, name, where, err)
}

// --- shared argument type ---------------------------------------------------

// NamedLookupArgs selects an object by name within an environment.
type NamedLookupArgs struct {
	// Name is the object's name.
	Name string `pulumi:"name"`
	// Environment is the Modal environment to look in. Falls back to the
	// provider-level environment, then the account default.
	Environment *string `pulumi:"environment,optional"`
}

func (a *NamedLookupArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.Name, "The object's name.")
	ann.Describe(&a.Environment, "The Modal environment to look in. Falls back to the provider-level environment, then the account's default environment.")
}

// --- getSecret ----------------------------------------------------------------

// GetSecret looks up an existing Secret.
type GetSecret struct{}

// GetSecretResult is the looked-up Secret. Modal never returns values.
type GetSecretResult struct {
	SecretId    string   `pulumi:"secretId"`
	Name        string   `pulumi:"name"`
	Environment string   `pulumi:"environment"`
	Keys        []string `pulumi:"keys"`
}

func (r *GetSecretResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.SecretId, "The Modal-assigned identifier of the Secret.")
	ann.Describe(&r.Name, "The Secret's name.")
	ann.Describe(&r.Environment, "The environment the Secret lives in.")
	ann.Describe(&r.Keys, "Sorted names of the keys stored in the Secret. Modal never returns the values.")
}

func (f *GetSecret) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal Secret by name. Returns its ID and key names; Modal never returns values.")
}

func (f *GetSecret) Invoke(ctx context.Context, req infer.FunctionRequest[NamedLookupArgs]) (infer.FunctionResponse[GetSecretResult], error) {
	out := infer.FunctionResponse[GetSecretResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := client.stub.SecretGetOrCreate(rpcCtx, pb.SecretGetOrCreateRequest_builder{
		DeploymentName:     req.Input.Name,
		EnvironmentName:    env,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
	}.Build())
	if err != nil {
		return out, lookupErr("secret", req.Input.Name, env, err)
	}
	info, err := client.stub.SecretGetInfo(rpcCtx, pb.SecretGetInfoRequest_builder{SecretId: got.GetSecretId()}.Build())
	if err != nil {
		return out, lookupErr("secret", req.Input.Name, env, err)
	}
	keys := slices.Clone(info.GetMetadata().GetKeys())
	sort.Strings(keys)
	if keys == nil {
		keys = []string{}
	}
	out.Output = GetSecretResult{
		SecretId: got.GetSecretId(), Name: req.Input.Name, Keys: keys,
		Environment: firstNonEmptyStr(info.GetMetadata().GetEnvironmentName(), env),
	}
	return out, nil
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// --- getVolume ----------------------------------------------------------------

// GetVolume looks up an existing Volume.
type GetVolume struct{}

// GetVolumeResult is the looked-up Volume.
type GetVolumeResult struct {
	VolumeId    string `pulumi:"volumeId"`
	Name        string `pulumi:"name"`
	Environment string `pulumi:"environment"`
	FsVersion   string `pulumi:"fsVersion"`
}

func (r *GetVolumeResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.VolumeId, "The Modal-assigned identifier of the Volume.")
	ann.Describe(&r.Name, "The Volume's name.")
	ann.Describe(&r.Environment, "The environment the object was found in: the one that was searched, else the account default environment.")
	ann.Describe(&r.FsVersion, "The Volume's filesystem version, \"v1\" or \"v2\".")
}

func (f *GetVolume) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal Volume by name.")
}

func (f *GetVolume) Invoke(ctx context.Context, req infer.FunctionRequest[NamedLookupArgs]) (infer.FunctionResponse[GetVolumeResult], error) {
	out := infer.FunctionResponse[GetVolumeResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := client.stub.VolumeGetOrCreate(rpcCtx, pb.VolumeGetOrCreateRequest_builder{
		DeploymentName:     req.Input.Name,
		EnvironmentName:    env,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
	}.Build())
	if err != nil {
		return out, lookupErr("volume", req.Input.Name, env, err)
	}
	byID, err := client.stub.VolumeGetById(rpcCtx, pb.VolumeGetByIdRequest_builder{VolumeId: got.GetVolumeId()}.Build())
	if err != nil {
		return out, lookupErr("volume", req.Input.Name, env, err)
	}
	out.Output = GetVolumeResult{
		VolumeId: got.GetVolumeId(), Name: req.Input.Name, Environment: lookupEnvironment(rpcCtx, client, env),
		FsVersion: fsVersionString(byID.GetMetadata().GetVersion()),
	}
	return out, nil
}

// --- getQueue -----------------------------------------------------------------

// GetQueue looks up an existing Queue.
type GetQueue struct{}

// GetQueueResult is the looked-up Queue.
type GetQueueResult struct {
	QueueId     string `pulumi:"queueId"`
	Name        string `pulumi:"name"`
	Environment string `pulumi:"environment"`
}

func (r *GetQueueResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.QueueId, "The Modal-assigned identifier of the Queue.")
	ann.Describe(&r.Name, "The Queue's name.")
	ann.Describe(&r.Environment, "The environment the object was found in: the one that was searched, else the account default environment.")
}

func (f *GetQueue) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal Queue by name.")
}

func (f *GetQueue) Invoke(ctx context.Context, req infer.FunctionRequest[NamedLookupArgs]) (infer.FunctionResponse[GetQueueResult], error) {
	out := infer.FunctionResponse[GetQueueResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := client.stub.QueueGetOrCreate(rpcCtx, pb.QueueGetOrCreateRequest_builder{
		DeploymentName:     req.Input.Name,
		EnvironmentName:    env,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
	}.Build())
	if err != nil {
		return out, lookupErr("queue", req.Input.Name, env, err)
	}
	out.Output = GetQueueResult{QueueId: got.GetQueueId(), Name: req.Input.Name, Environment: lookupEnvironment(rpcCtx, client, env)}
	return out, nil
}

// --- getDict ------------------------------------------------------------------

// GetDict looks up an existing Dict.
type GetDict struct{}

// GetDictResult is the looked-up Dict.
type GetDictResult struct {
	DictId      string `pulumi:"dictId"`
	Name        string `pulumi:"name"`
	Environment string `pulumi:"environment"`
}

func (r *GetDictResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.DictId, "The Modal-assigned identifier of the Dict.")
	ann.Describe(&r.Name, "The Dict's name.")
	ann.Describe(&r.Environment, "The environment the object was found in: the one that was searched, else the account default environment.")
}

func (f *GetDict) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal Dict by name.")
}

func (f *GetDict) Invoke(ctx context.Context, req infer.FunctionRequest[NamedLookupArgs]) (infer.FunctionResponse[GetDictResult], error) {
	out := infer.FunctionResponse[GetDictResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := client.stub.DictGetOrCreate(rpcCtx, pb.DictGetOrCreateRequest_builder{
		DeploymentName:     req.Input.Name,
		EnvironmentName:    env,
		ObjectCreationType: pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED,
	}.Build())
	if err != nil {
		return out, lookupErr("dict", req.Input.Name, env, err)
	}
	out.Output = GetDictResult{DictId: got.GetDictId(), Name: req.Input.Name, Environment: lookupEnvironment(rpcCtx, client, env)}
	return out, nil
}

// --- getEnvironment -----------------------------------------------------------

// GetEnvironment looks up an environment of the workspace.
type GetEnvironment struct{}

// GetEnvironmentArgs selects an environment by name.
type GetEnvironmentArgs struct {
	// Name is the environment's name.
	Name string `pulumi:"name"`
}

func (a *GetEnvironmentArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.Name, "The environment's name.")
}

// GetEnvironmentResult is the looked-up environment.
type GetEnvironmentResult struct {
	EnvironmentId      string `pulumi:"environmentId"`
	Name               string `pulumi:"name"`
	WebSuffix          string `pulumi:"webSuffix"`
	IsDefault          bool   `pulumi:"isDefault"`
	MaxConcurrentTasks *int   `pulumi:"maxConcurrentTasks,optional"`
	MaxConcurrentGpus  *int   `pulumi:"maxConcurrentGpus,optional"`
}

func (r *GetEnvironmentResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.EnvironmentId, "The Modal-assigned identifier of the environment.")
	ann.Describe(&r.Name, "The environment's name.")
	ann.Describe(&r.WebSuffix, "Suffix of web endpoint URLs created in the environment.")
	ann.Describe(&r.IsDefault, "Whether this is the workspace's default environment.")
	ann.Describe(&r.MaxConcurrentTasks, "Maximum tasks running at once; unset when unlimited.")
	ann.Describe(&r.MaxConcurrentGpus, "Maximum GPUs in use at once; unset when unlimited.")
}

func (f *GetEnvironment) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal environment by name.")
}

func (f *GetEnvironment) Invoke(ctx context.Context, req infer.FunctionRequest[GetEnvironmentArgs]) (infer.FunctionResponse[GetEnvironmentResult], error) {
	out := infer.FunctionResponse[GetEnvironmentResult]{}
	client, _, closeFn, err := lookupSession(ctx, nil)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	item, err := findEnvironment(rpcCtx, client, "", req.Input.Name)
	if err != nil {
		return out, fmt.Errorf("modal: look up environment %q: %w", req.Input.Name, err)
	}
	if item == nil {
		return out, fmt.Errorf("modal: environment %q was not found", req.Input.Name)
	}
	out.Output = GetEnvironmentResult{
		EnvironmentId: item.GetEnvironmentId(), Name: item.GetName(), WebSuffix: item.GetWebhookSuffix(),
		IsDefault: item.GetDefault(), MaxConcurrentTasks: optInt(item.HasMaxConcurrentTasks(), item.GetMaxConcurrentTasks()), MaxConcurrentGpus: optInt(item.HasMaxConcurrentGpus(), item.GetMaxConcurrentGpus()),
	}
	return out, nil
}

// --- getApp -------------------------------------------------------------------

// GetApp looks up an existing App.
type GetApp struct{}

// GetAppResult is the looked-up App.
type GetAppResult struct {
	AppId       string            `pulumi:"appId"`
	Name        string            `pulumi:"name"`
	Environment string            `pulumi:"environment"`
	Tags        map[string]string `pulumi:"tags"`
}

func (r *GetAppResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.AppId, "The Modal-assigned identifier of the App.")
	ann.Describe(&r.Name, "The App's name.")
	ann.Describe(&r.Environment, "The environment the App lives in.")
	ann.Describe(&r.Tags, "The tags set on the App.")
}

func (f *GetApp) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal App by name (including Apps created by `modal deploy`).")
}

func (f *GetApp) Invoke(ctx context.Context, req infer.FunctionRequest[NamedLookupArgs]) (infer.FunctionResponse[GetAppResult], error) {
	out := infer.FunctionResponse[GetAppResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	// The live API answers a lookup of a missing App with success and an empty
	// ID, so lookupAppID reports that as not found too.
	got, err := lookupApp(rpcCtx, client, req.Input.Name, env)
	if err != nil {
		return out, lookupErr("app", req.Input.Name, env, err)
	}
	if got == nil {
		return out, lookupErr("app", req.Input.Name, env, status.Error(codes.NotFound, "app not found"))
	}
	tags, err := client.stub.AppGetTags(rpcCtx, pb.AppGetTagsRequest_builder{AppId: got.GetAppId()}.Build())
	if err != nil {
		return out, lookupErr("app", req.Input.Name, env, err)
	}
	t := tags.GetTags()
	if t == nil {
		t = map[string]string{}
	}
	out.Output = GetAppResult{
		AppId: got.GetAppId(), Name: req.Input.Name, Tags: t,
		Environment: firstNonEmptyStr(got.GetEnvironmentName(), env),
	}
	return out, nil
}

// --- getImage -----------------------------------------------------------------

// GetImage looks up a published Image.
type GetImage struct{}

// GetImageResult is the looked-up Image.
type GetImageResult struct {
	ImageId string `pulumi:"imageId"`
	Name    string `pulumi:"name"`
}

func (r *GetImageResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.ImageId, "The Modal-assigned identifier of the Image.")
	ann.Describe(&r.Name, "The published name with its tag, \"name:tag\".")
}

func (f *GetImage) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up a published Modal Image by name (\"name\" or \"name:tag\", tag defaults to latest).")
}

func (f *GetImage) Invoke(ctx context.Context, req infer.FunctionRequest[NamedLookupArgs]) (infer.FunctionResponse[GetImageResult], error) {
	out := infer.FunctionResponse[GetImageResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	tag := imageTag(req.Input.Name)
	got, err := client.stub.ImageGetByTag(rpcCtx, pb.ImageGetByTagRequest_builder{Tag: tag, EnvironmentName: env}.Build())
	if err != nil {
		return out, lookupErr("image", tag, env, err)
	}
	out.Output = GetImageResult{ImageId: got.GetImageId(), Name: tag}
	return out, nil
}

// --- getFunction --------------------------------------------------------------

// GetFunction looks up a Function deployed in an App.
type GetFunction struct{}

// GetFunctionArgs selects a deployed Function.
type GetFunctionArgs struct {
	// AppName is the name of the App the Function is deployed in.
	AppName string `pulumi:"appName"`
	// FunctionName is the Function's name within the App.
	FunctionName string `pulumi:"functionName"`
	// Environment is the Modal environment to look in.
	Environment *string `pulumi:"environment,optional"`
}

func (a *GetFunctionArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.AppName, "The name of the App the Function is deployed in.")
	ann.Describe(&a.FunctionName, "The Function's name within the App.")
	ann.Describe(&a.Environment, "The Modal environment to look in. Falls back to the provider-level environment, then the account's default environment.")
}

// GetFunctionResult is the looked-up Function.
type GetFunctionResult struct {
	FunctionId string `pulumi:"functionId"`
	WebUrl     string `pulumi:"webUrl"`
}

func (r *GetFunctionResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.FunctionId, "The Modal-assigned identifier of the Function.")
	ann.Describe(&r.WebUrl, "The URL of the Function's web endpoint; empty when it has none.")
}

func (f *GetFunction) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up a deployed Modal Function. Deploying code is out of scope for this provider; deploy with `modal deploy` and reference the result here.")
}

func (f *GetFunction) Invoke(ctx context.Context, req infer.FunctionRequest[GetFunctionArgs]) (infer.FunctionResponse[GetFunctionResult], error) {
	out := infer.FunctionResponse[GetFunctionResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := client.stub.FunctionGet(rpcCtx, pb.FunctionGetRequest_builder{
		AppName: req.Input.AppName, ObjectTag: req.Input.FunctionName, EnvironmentName: env,
	}.Build())
	if err != nil {
		return out, lookupErr("function", req.Input.AppName+"."+req.Input.FunctionName, env, err)
	}
	out.Output = GetFunctionResult{FunctionId: got.GetFunctionId(), WebUrl: got.GetHandleMetadata().GetWebUrl()}
	return out, nil
}

// --- getCls -------------------------------------------------------------------

// GetCls looks up a Cls (class) deployed in an App.
type GetCls struct{}

// GetClsArgs selects a deployed Cls.
type GetClsArgs struct {
	// AppName is the name of the App the Cls is deployed in.
	AppName string `pulumi:"appName"`
	// ClsName is the Cls's name within the App.
	ClsName string `pulumi:"clsName"`
	// Environment is the Modal environment to look in.
	Environment *string `pulumi:"environment,optional"`
}

func (a *GetClsArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.AppName, "The name of the App the Cls is deployed in.")
	ann.Describe(&a.ClsName, "The Cls's name within the App.")
	ann.Describe(&a.Environment, "The Modal environment to look in. Falls back to the provider-level environment, then the account's default environment.")
}

// GetClsResult is the looked-up Cls.
type GetClsResult struct {
	ClsId   string   `pulumi:"clsId"`
	Methods []string `pulumi:"methods"`
}

func (r *GetClsResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.ClsId, "The Modal-assigned identifier of the Cls.")
	ann.Describe(&r.Methods, "Sorted names of the Cls's methods.")
}

func (f *GetCls) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up a deployed Modal Cls (class). Deploying code is out of scope for this provider.")
}

func (f *GetCls) Invoke(ctx context.Context, req infer.FunctionRequest[GetClsArgs]) (infer.FunctionResponse[GetClsResult], error) {
	out := infer.FunctionResponse[GetClsResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := client.stub.ClassGet(rpcCtx, pb.ClassGetRequest_builder{
		AppName: req.Input.AppName, ObjectTag: req.Input.ClsName, EnvironmentName: env,
	}.Build())
	if err != nil {
		return out, lookupErr("cls", req.Input.AppName+"."+req.Input.ClsName, env, err)
	}
	methods := []string{}
	for _, m := range got.GetHandleMetadata().GetMethods() {
		methods = append(methods, m.GetFunctionName())
	}
	sort.Strings(methods)
	out.Output = GetClsResult{ClsId: got.GetClassId(), Methods: methods}
	return out, nil
}

// --- getProxy -----------------------------------------------------------------

// GetProxy looks up a static-IP Proxy.
type GetProxy struct{}

// GetProxyResult is the looked-up Proxy.
type GetProxyResult struct {
	ProxyId string   `pulumi:"proxyId"`
	Name    string   `pulumi:"name"`
	Region  string   `pulumi:"region"`
	Ips     []string `pulumi:"ips"`
}

func (r *GetProxyResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.ProxyId, "The Modal-assigned identifier of the Proxy.")
	ann.Describe(&r.Name, "The Proxy's name.")
	ann.Describe(&r.Region, "The region the Proxy runs in.")
	ann.Describe(&r.Ips, "The Proxy's static IP addresses.")
}

func (f *GetProxy) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up an existing Modal Proxy (static outbound IPs) by name. Proxies are created in the Modal dashboard.")
}

func (f *GetProxy) Invoke(ctx context.Context, req infer.FunctionRequest[NamedLookupArgs]) (infer.FunctionResponse[GetProxyResult], error) {
	out := infer.FunctionResponse[GetProxyResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := client.stub.ProxyGet(rpcCtx, pb.ProxyGetRequest_builder{Name: req.Input.Name, EnvironmentName: env}.Build())
	if err != nil {
		return out, lookupErr("proxy", req.Input.Name, env, err)
	}
	p := got.GetProxy()
	ips := []string{}
	for _, ip := range p.GetProxyIps() {
		ips = append(ips, ip.GetProxyIp())
	}
	sort.Strings(ips)
	out.Output = GetProxyResult{ProxyId: p.GetProxyId(), Name: p.GetName(), Region: p.GetRegion(), Ips: ips}
	return out, nil
}

// --- getWorkspace -------------------------------------------------------------

// GetWorkspace describes the workspace the provider's token belongs to.
type GetWorkspace struct{}

// GetWorkspaceArgs has no arguments.
type GetWorkspaceArgs struct{}

// GetWorkspaceResult describes the token's workspace.
type GetWorkspaceResult struct {
	WorkspaceId         string `pulumi:"workspaceId"`
	WorkspaceName       string `pulumi:"workspaceName"`
	TokenId             string `pulumi:"tokenId"`
	TokenName           string `pulumi:"tokenName"`
	DefaultEnvironment  string `pulumi:"defaultEnvironment"`
	ImageBuilderVersion string `pulumi:"imageBuilderVersion"`
}

func (r *GetWorkspaceResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.WorkspaceId, "The identifier of the workspace the token belongs to.")
	ann.Describe(&r.WorkspaceName, "The workspace's name.")
	ann.Describe(&r.TokenId, "The ID of the token the provider authenticates with.")
	ann.Describe(&r.TokenName, "The name of that token.")
	ann.Describe(&r.DefaultEnvironment, "The workspace's default environment.")
	ann.Describe(&r.ImageBuilderVersion, "The workspace's image builder version.")
}

func (f *GetWorkspace) Annotate(a infer.Annotator) {
	a.Describe(&f, "Describe the Modal workspace the provider's token belongs to.")
}

func (f *GetWorkspace) Invoke(ctx context.Context, _ infer.FunctionRequest[GetWorkspaceArgs]) (infer.FunctionResponse[GetWorkspaceResult], error) {
	out := infer.FunctionResponse[GetWorkspaceResult]{}
	client, _, closeFn, err := lookupSession(ctx, nil)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	info, err := client.stub.TokenInfoGet(rpcCtx, pb.TokenInfoGetRequest_builder{}.Build())
	if err != nil {
		return out, fmt.Errorf("modal: look up workspace: %w", err)
	}
	settings, err := client.stub.WorkspaceSettings(rpcCtx, emptyReq())
	if err != nil {
		return out, fmt.Errorf("modal: read workspace settings: %w", err)
	}
	out.Output = GetWorkspaceResult{
		WorkspaceId: info.GetWorkspaceId(), WorkspaceName: info.GetWorkspaceName(),
		TokenId: info.GetTokenId(), TokenName: info.GetTokenName(),
		DefaultEnvironment: settings.GetDefaultEnvironmentName(), ImageBuilderVersion: settings.GetImageBuilderVersion(),
	}
	return out, nil
}
