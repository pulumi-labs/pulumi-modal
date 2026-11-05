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

	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ProviderConfig holds provider-level configuration shared across all
// resources. Every field can be overridden per-resource.
type ProviderConfig struct {
	// TokenID is the Modal API token ID. Falls back to MODAL_TOKEN_ID, then
	// to the active profile of ~/.modal.toml.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`

	// TokenSecret is the Modal API token secret. Falls back to
	// MODAL_TOKEN_SECRET, then to the active profile of ~/.modal.toml.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Environment is the default Modal environment (workspace sub-namespace)
	// resources are created in when not overridden per-resource. Falls back
	// to MODAL_ENVIRONMENT, then to the `environment` of the ~/.modal.toml
	// profile in use (only when that profile supplies the credentials), then
	// to the account's default environment.
	Environment *string `pulumi:"environment,optional"`

	// ServerAddr overrides Modal's gRPC control-plane endpoint. Intended for
	// testing against a mock server; almost never set in production.
	ServerAddr *string `pulumi:"serverAddr,optional"`
}

// Annotate describes the provider config fields and wires their environment
// variable fallbacks; infer reads env defaults only from SetDefault, not from
// struct tags.
func (c *ProviderConfig) Annotate(a infer.Annotator) {
	a.SetDefault(&c.TokenID, nil, "MODAL_TOKEN_ID")
	a.SetDefault(&c.TokenSecret, nil, "MODAL_TOKEN_SECRET")
	a.SetDefault(&c.Environment, nil, "MODAL_ENVIRONMENT")
	a.SetDefault(&c.ServerAddr, nil, "MODAL_SERVER_ADDR")
	a.Describe(&c.TokenID, "Modal API token ID. Falls back to the MODAL_TOKEN_ID environment variable, then to the active profile of ~/.modal.toml (MODAL_CONFIG_PATH, MODAL_PROFILE) written by `modal token new`.")
	a.Describe(&c.TokenSecret, "Modal API token secret. Falls back to the MODAL_TOKEN_SECRET environment variable, then to the active profile of ~/.modal.toml (MODAL_CONFIG_PATH, MODAL_PROFILE) written by `modal token new`.")
	a.Describe(&c.Environment, "Default Modal environment (workspace namespace) for resources that don't set their own. Falls back to MODAL_ENVIRONMENT, then the environment of the ~/.modal.toml profile that supplies the credentials, then the account's default environment.")
	a.Describe(&c.ServerAddr, "Override for Modal's gRPC control-plane endpoint. Only used for testing.")
}

// resolveStr returns the resource-level override if set, else the
// provider-level config value, else "".
func resolveStr(resourceLevel *string, providerLevel *string) string {
	if resourceLevel != nil && *resourceLevel != "" {
		return *resourceLevel
	}
	if providerLevel != nil {
		return *providerLevel
	}
	return ""
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// environmentChanged reports whether the environment the program asks for
// differs from the environment recorded in state. State holds the effective
// environment written at create time. An unset desired environment means
// "whatever the account default is" and never forces a replace, and a state
// that predates environment tracking (empty) cannot be compared.
func environmentChanged(input *string, state *string, cfg ProviderConfig) bool {
	desired := resolveEnv(input, cfg)
	recorded := derefStr(state)
	return desired != "" && recorded != "" && desired != recorded
}

// effectiveEnvironment returns the environment an object will be created in:
// the resource-level value, else the provider-level value, else the account's
// default environment as reported by Modal. Looking up the default is best
// effort; on failure it returns "" and the caller records nothing.
func effectiveEnvironment(ctx context.Context, client *modalClient, input *string, cfg ProviderConfig) string {
	if env := resolveEnv(input, cfg); env != "" {
		return env
	}
	resp, err := client.stub.EnvironmentList(ctx, &emptypb.Empty{})
	if err != nil {
		return ""
	}
	for _, item := range resp.GetItems() {
		if item.GetDefault() {
			return item.GetName()
		}
	}
	return ""
}

func emptyReq() *emptypb.Empty { return &emptypb.Empty{} }

func isNotFound(err error) bool { return status.Code(err) == codes.NotFound }

// clientFor builds a modalClient from the effective provider config and any
// resource-level token overrides.
func clientFor(cfg ProviderConfig, resourceTokenID, resourceTokenSecret *string) (*modalClient, error) {
	tokenID := resolveStr(resourceTokenID, cfg.TokenID)
	tokenSecret := resolveStr(resourceTokenSecret, cfg.TokenSecret)
	serverAddr := resolveStr(nil, cfg.ServerAddr)

	// Last source: the profile file the Modal CLI writes. Only consulted when
	// nothing above supplied a token.
	var prof *modalProfile
	if tokenID == "" && tokenSecret == "" {
		var err error
		if prof, err = loadModalProfile(); err != nil {
			return nil, err
		}
		if prof != nil {
			tokenID, tokenSecret = prof.TokenID, prof.TokenSecret
			if serverAddr == "" {
				serverAddr = prof.serverAddr()
			}
		}
	}
	if tokenID == "" || tokenSecret == "" {
		return nil, missingCredentialsError(tokenID, tokenSecret, prof)
	}
	return newModalClient(tokenID, tokenSecret, serverAddr)
}

// New returns the configured Modal Pulumi provider.
func New() (pgp.Provider, error) {
	prov, err := infer.NewProviderBuilder().
		WithDisplayName("Modal").
		WithDescription(
			"Experimental: manage Modal (modal.com) serverless compute resources — Secrets, Volumes, Queues, Dicts, Environments, Apps and Images — as infrastructure with Pulumi.",
		).
		WithHomepage("https://modal.com").
		WithRepository("https://github.com/pulumi-labs/pulumi-modal").
		WithPublisher("Pulumi Labs").
		WithLanguageMap(map[string]any{
			"go": map[string]any{"generateResourceContainerTypes": true, "respectSchemaVersion": true},
			"nodejs": map[string]any{
				"packageName":          "@pulumi-labs/modal",
				"packageDescription":   "Experimental: manage Modal (modal.com) serverless compute resources — Secrets, Volumes, Queues, Dicts, Environments, Apps and Images — as infrastructure with Pulumi.",
				"respectSchemaVersion": true,
			},
			"python": map[string]any{
				"packageName":          "pulumi_labs_modal",
				"respectSchemaVersion": true,
				"pyproject":            map[string]any{"enabled": true},
			},
			"csharp": map[string]any{"rootNamespace": "Pulumi.Labs", "respectSchemaVersion": true},
			"java":   map[string]any{"basePackage": "com.pulumi.labs", "respectSchemaVersion": true},
		}).
		WithGoImportPath("github.com/pulumi-labs/pulumi-modal/sdk/go/modal").
		WithPluginDownloadURL("github://api.github.com/pulumi-labs/pulumi-modal").
		WithLicense("Apache-2.0").
		WithKeywords("pulumi", "modal", "serverless", "ai", "compute", "category/cloud").
		WithModuleMap(map[tokens.ModuleName]tokens.ModuleName{
			"provider": "index",
		}).
		WithConfig(infer.Config(ProviderConfig{})).
		WithResources(
			infer.Resource[*Secret, SecretArgs, SecretState](&Secret{}),
			infer.Resource[*Volume, VolumeArgs, VolumeState](&Volume{}),
			infer.Resource[*Queue, QueueArgs, QueueState](&Queue{}),
			infer.Resource[*Dict, DictArgs, DictState](&Dict{}),
			infer.Resource[*Environment, EnvironmentArgs, EnvironmentState](&Environment{}),
			infer.Resource[*App, AppArgs, AppState](&App{}),
			infer.Resource[*Image, ImageArgs, ImageState](&Image{}),
		).
		WithFunctions(
			infer.Function[*GetSecret, NamedLookupArgs, GetSecretResult](&GetSecret{}),
			infer.Function[*GetVolume, NamedLookupArgs, GetVolumeResult](&GetVolume{}),
			infer.Function[*GetQueue, NamedLookupArgs, GetQueueResult](&GetQueue{}),
			infer.Function[*GetDict, NamedLookupArgs, GetDictResult](&GetDict{}),
			infer.Function[*GetEnvironment, GetEnvironmentArgs, GetEnvironmentResult](&GetEnvironment{}),
			infer.Function[*GetApp, NamedLookupArgs, GetAppResult](&GetApp{}),
			infer.Function[*GetImage, NamedLookupArgs, GetImageResult](&GetImage{}),
			infer.Function[*GetFunction, GetFunctionArgs, GetFunctionResult](&GetFunction{}),
			infer.Function[*GetCls, GetClsArgs, GetClsResult](&GetCls{}),
			infer.Function[*GetProxy, NamedLookupArgs, GetProxyResult](&GetProxy{}),
			infer.Function[*GetWorkspace, GetWorkspaceArgs, GetWorkspaceResult](&GetWorkspace{}),
		).
		Build()
	if err != nil {
		return pgp.Provider{}, err
	}
	// TODO(pulumi/pulumi-go-provider#710): drop this wrapper once infer.DiffRequest exposes OldInputs.
	// Surface the engine's old inputs to Secret.Diff; see withOldInputs.
	diff := prov.Diff
	prov.Diff = func(ctx context.Context, req pgp.DiffRequest) (pgp.DiffResponse, error) {
		return diff(withOldInputs(ctx, req.OldInputs), req)
	}
	return prov, nil
}
