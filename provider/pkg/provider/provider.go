package provider

import (
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
)

// ProviderConfig holds provider-level configuration shared across all
// resources. Every field can be overridden per-resource.
type ProviderConfig struct {
	// TokenID is the Modal API token ID. Falls back to MODAL_TOKEN_ID.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`

	// TokenSecret is the Modal API token secret. Falls back to MODAL_TOKEN_SECRET.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Environment is the default Modal environment (workspace sub-namespace)
	// resources are created in when not overridden per-resource. Falls back
	// to MODAL_ENVIRONMENT, then to the account's default environment.
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
	a.Describe(&c.TokenID, "Modal API token ID. Falls back to the MODAL_TOKEN_ID environment variable.")
	a.Describe(&c.TokenSecret, "Modal API token secret. Falls back to the MODAL_TOKEN_SECRET environment variable.")
	a.Describe(&c.Environment, "Default Modal environment (workspace namespace) for resources that don't set their own. Falls back to MODAL_ENVIRONMENT, then the account's default environment.")
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

// clientFor builds a modalClient from the effective provider config and any
// resource-level token overrides.
func clientFor(cfg ProviderConfig, resourceTokenID, resourceTokenSecret *string) (*modalClient, error) {
	tokenID := resolveStr(resourceTokenID, cfg.TokenID)
	tokenSecret := resolveStr(resourceTokenSecret, cfg.TokenSecret)
	serverAddr := resolveStr(nil, cfg.ServerAddr)
	return newModalClient(tokenID, tokenSecret, serverAddr)
}

// New returns the configured Modal Pulumi provider.
func New() (pgp.Provider, error) {
	return infer.NewProviderBuilder().
		WithDisplayName("Modal").
		WithDescription(
			"Manage Modal (modal.com) serverless compute resources — Secrets, Volumes, and Queues — as infrastructure with Pulumi.",
		).
		WithHomepage("https://modal.com").
		WithRepository("https://github.com/pulumi/autoregistry").
		WithGoImportPath("github.com/pulumi/pulumi-modal/sdk/go/modal").
		WithPublisher("Pulumi").
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
		).
		Build()
}
