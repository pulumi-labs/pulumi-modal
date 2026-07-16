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
	TokenID *string `pulumi:"tokenId,optional" provider:"secret" env:"MODAL_TOKEN_ID"`

	// TokenSecret is the Modal API token secret. Falls back to MODAL_TOKEN_SECRET.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret" env:"MODAL_TOKEN_SECRET"`

	// Environment is the default Modal environment (workspace sub-namespace)
	// resources are created in when not overridden per-resource. Falls back
	// to MODAL_ENVIRONMENT, then to the account's default environment.
	Environment *string `pulumi:"environment,optional" env:"MODAL_ENVIRONMENT"`

	// ServerAddr overrides Modal's gRPC control-plane endpoint. Intended for
	// testing against a mock server; almost never set in production.
	ServerAddr *string `pulumi:"serverAddr,optional" env:"MODAL_SERVER_ADDR"`
}

func (c *ProviderConfig) Annotate(a infer.Annotator) {
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
		WithDisplayName("modal").
		WithDescription(
			"Manage Modal (modal.com) serverless compute resources — Secrets, Volumes, and Queues — as infrastructure with Pulumi.",
		).
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
