package provider

import (
	"testing"

	"github.com/blang/semver"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Provider construction
// ---------------------------------------------------------------------------

// TestProvider_DoesNotPanic verifies that provider construction succeeds
// without error. New() (via infer.NewProviderBuilder) fails if the infer
// framework detects a schema inconsistency — e.g. duplicate type tokens,
// missing required tags, or a resource whose args/state don't satisfy the
// infer constraints.
func TestProvider_DoesNotPanic(t *testing.T) {
	_, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ProviderConfig
// ---------------------------------------------------------------------------

// TestProviderConfig_DefaultsNil is a compile-time and runtime check that
// ProviderConfig's optional fields default to nil (not require explicit
// zero-value handling elsewhere).
func TestProviderConfig_DefaultsNil(t *testing.T) {
	cfg := ProviderConfig{}
	if cfg.TokenID != nil {
		t.Error("TokenID should default to nil")
	}
	if cfg.TokenSecret != nil {
		t.Error("TokenSecret should default to nil")
	}
	if cfg.Environment != nil {
		t.Error("Environment should default to nil")
	}
	if cfg.ServerAddr != nil {
		t.Error("ServerAddr should default to nil")
	}
}

// ---------------------------------------------------------------------------
// resolveStr — resource-vs-provider override priority
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

// TestResolveStr_ResourceWins verifies that a non-empty resource-level value
// takes priority over the provider-level config value.
func TestResolveStr_ResourceWins(t *testing.T) {
	resource := strPtr("resource-value")
	providerLevel := strPtr("provider-value")
	got := resolveStr(resource, providerLevel)
	if got != "resource-value" {
		t.Errorf("resource value should win: got %q, want %q", got, "resource-value")
	}
}

// TestResolveStr_FallsBackToProvider verifies that a nil resource-level value
// falls back to the provider-level config value.
func TestResolveStr_FallsBackToProvider(t *testing.T) {
	providerLevel := strPtr("provider-value")
	got := resolveStr(nil, providerLevel)
	if got != "provider-value" {
		t.Errorf("should fall back to provider value: got %q, want %q", got, "provider-value")
	}
}

// TestResolveStr_EmptyResourceFallsBackToProvider verifies that an
// explicitly empty-string resource-level pointer is treated the same as nil
// and falls back to the provider-level value, rather than short-circuiting
// on an empty string.
func TestResolveStr_EmptyResourceFallsBackToProvider(t *testing.T) {
	empty := strPtr("")
	providerLevel := strPtr("provider-value")
	got := resolveStr(empty, providerLevel)
	if got != "provider-value" {
		t.Errorf("empty resource value should fall back to provider value: got %q, want %q", got, "provider-value")
	}
}

// TestResolveStr_NeitherSetReturnsEmpty verifies that when neither the
// resource-level nor the provider-level value is set, resolveStr returns the
// empty string rather than panicking on a nil dereference.
func TestResolveStr_NeitherSetReturnsEmpty(t *testing.T) {
	got := resolveStr(nil, nil)
	if got != "" {
		t.Errorf("expected empty string when neither value is set, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// clientFor — token/serverAddr resolution wiring
// ---------------------------------------------------------------------------

// TestClientFor_UsesResourceOverrideTokens verifies that clientFor prefers
// resource-level token overrides over provider-level config, and that a
// client is constructed without error given valid (non-empty) credentials.
func TestClientFor_UsesResourceOverrideTokens(t *testing.T) {
	cfg := ProviderConfig{
		TokenID:     strPtr("provider-token-id"),
		TokenSecret: strPtr("provider-token-secret"),
	}
	resourceTokenID := strPtr("resource-token-id")
	resourceTokenSecret := strPtr("resource-token-secret")

	client, err := clientFor(cfg, resourceTokenID, resourceTokenSecret)
	if err != nil {
		t.Fatalf("clientFor() returned error: %v", err)
	}
	if client == nil {
		t.Fatal("clientFor() returned nil client with no error")
	}
}

// TestClientFor_FallsBackToProviderTokens verifies that clientFor falls
// back to provider-level tokens when no resource-level override is given.
func TestClientFor_FallsBackToProviderTokens(t *testing.T) {
	cfg := ProviderConfig{
		TokenID:     strPtr("provider-token-id"),
		TokenSecret: strPtr("provider-token-secret"),
	}

	client, err := clientFor(cfg, nil, nil)
	if err != nil {
		t.Fatalf("clientFor() returned error: %v", err)
	}
	if client == nil {
		t.Fatal("clientFor() returned nil client with no error")
	}
}

// ---------------------------------------------------------------------------
// Resource args — pulumi struct tags (schema shape smoke tests)
// ---------------------------------------------------------------------------

// TestSecretArgs_RequiredFields is a compile-time check that SecretArgs has
// the expected field shape: Name and Values are required (non-pointer),
// while TokenID/TokenSecret/Environment are optional (pointer) overrides.
func TestSecretArgs_RequiredFields(t *testing.T) {
	args := SecretArgs{
		Name:   "my-secret",
		Values: map[string]string{"KEY": "value"},
	}
	if args.Name != "my-secret" {
		t.Errorf("Name = %q, want %q", args.Name, "my-secret")
	}
	if len(args.Values) != 1 {
		t.Errorf("Values length = %d, want 1", len(args.Values))
	}
	if args.TokenID != nil || args.TokenSecret != nil || args.Environment != nil {
		t.Error("optional override fields should default to nil")
	}
}

// TestVolumeArgs_RequiredFields is a compile-time check that VolumeArgs has
// the expected field shape: Name is required, Version/Environment/token
// overrides are optional.
func TestVolumeArgs_RequiredFields(t *testing.T) {
	args := VolumeArgs{
		Name:    "my-volume",
		Version: strPtr("v2"),
	}
	if args.Name != "my-volume" {
		t.Errorf("Name = %q, want %q", args.Name, "my-volume")
	}
	if args.Version == nil || *args.Version != "v2" {
		t.Error("Version should be set to v2")
	}
}

// TestQueueArgs_RequiredFields is a compile-time check that QueueArgs has
// the expected field shape: Name is required, Environment/token overrides
// are optional.
func TestQueueArgs_RequiredFields(t *testing.T) {
	args := QueueArgs{
		Name: "my-queue",
	}
	if args.Name != "my-queue" {
		t.Errorf("Name = %q, want %q", args.Name, "my-queue")
	}
	if args.Environment != nil {
		t.Error("Environment should default to nil")
	}
}

// ---------------------------------------------------------------------------
// Diff — provider-level environment resolution (regression coverage)
// ---------------------------------------------------------------------------
//
// These tests exercise Diff through the real provider RPC surface (via
// integration.Server), with a provider-level "environment" configured and a
// nil resource-level environment on one side of the comparison. Before the
// fix, Diff compared resolveStr(req.Inputs.Environment, nil) against
// resolveStr(req.State.Environment, nil) — never consulting the provider
// config — so a resource-level environment value that merely restates the
// provider-level default (a common refactor: "make the previously-implicit
// environment explicit") looked like a change and forced a replace. Volume
// and Queue have no Update path, so that spurious replace is a data-loss
// bug. Secret would silently trigger an unnecessary destroy/create too.
//
// urn/testServer/testProviderConfig below build the minimal harness needed
// to drive Diff with a live provider config, mirroring the pattern used in
// pulumi-go-provider's own infer/tests package (providerWithConfig +
// integration.Server).

func modalUrn(typ, name string) resource.URN {
	return resource.NewURN("stack", "proj", "", tokens.Type("modal:index:"+typ), name)
}

// newTestServer builds a live modal provider wrapped for RPC-level testing
// and configures it with the given provider-level environment.
func newTestServer(t *testing.T, providerEnvironment string) integration.Server {
	t.Helper()

	p, err := New()
	require.NoError(t, err)

	s, err := integration.NewServer(t.Context(), "modal", semver.MustParse("0.0.1"), integration.WithProvider(p))
	require.NoError(t, err)

	err = s.Configure(pgp.ConfigureRequest{
		Args: property.NewMap(map[string]property.Value{
			"environment": property.New(providerEnvironment),
		}),
	})
	require.NoError(t, err)

	return s
}

// TestSecretDiff_ProviderLevelEnvironment_NoSpuriousReplace verifies that
// Diff does not flag "environment" as changed when the resource-level input
// merely restates the already-active provider-level environment and the
// prior state relied on that provider default implicitly (nil).
func TestSecretDiff_ProviderLevelEnvironment_NoSpuriousReplace(t *testing.T) {
	s := newTestServer(t, "prod-env")

	diff, err := s.Diff(pgp.DiffRequest{
		Urn: modalUrn("Secret", "mysecret"),
		State: property.NewMap(map[string]property.Value{
			"name": property.New("mysecret"),
			"values": property.New(property.NewMap(map[string]property.Value{
				"KEY": property.New("value"),
			})),
			"secretId": property.New("sec-123"),
		}),
		Inputs: property.NewMap(map[string]property.Value{
			"name": property.New("mysecret"),
			"values": property.New(property.NewMap(map[string]property.Value{
				"KEY": property.New("value"),
			})),
			"environment": property.New("prod-env"),
		}),
	})
	require.NoError(t, err)

	if diff.HasChanges {
		t.Errorf("expected no changes when resource-level environment matches the active provider-level "+
			"environment, got HasChanges=true with detailed diff: %+v", diff.DetailedDiff)
	}
	if _, ok := diff.DetailedDiff["environment"]; ok {
		t.Error("environment should not appear in DetailedDiff when it resolves to the same effective value " +
			"on both sides")
	}
}

// TestVolumeDiff_ProviderLevelEnvironment_NoSpuriousReplace mirrors the
// Secret case for Volume, which (like Queue) has no Update path — a
// spurious replace here is destructive, not just wasteful.
func TestVolumeDiff_ProviderLevelEnvironment_NoSpuriousReplace(t *testing.T) {
	s := newTestServer(t, "prod-env")

	// Deliberately omit "version" from both sides: the infer framework's
	// diff() helper strips any input property literally named "version"
	// unconditionally (it assumes that key is the engine-reserved schema
	// version, not a resource field), which would make Version look changed
	// on every Diff call regardless of the environment fix under test here.
	// That's a separate, pre-existing quirk unrelated to environment
	// resolution, so it is excluded to keep this test isolated.
	diff, err := s.Diff(pgp.DiffRequest{
		Urn: modalUrn("Volume", "myvolume"),
		State: property.NewMap(map[string]property.Value{
			"name":     property.New("myvolume"),
			"volumeId": property.New("vol-123"),
		}),
		Inputs: property.NewMap(map[string]property.Value{
			"name":        property.New("myvolume"),
			"environment": property.New("prod-env"),
		}),
	})
	require.NoError(t, err)

	if diff.HasChanges {
		t.Errorf("expected no changes when resource-level environment matches the active provider-level "+
			"environment, got HasChanges=true with detailed diff: %+v", diff.DetailedDiff)
	}
	if _, ok := diff.DetailedDiff["environment"]; ok {
		t.Error("environment should not appear in DetailedDiff when it resolves to the same effective value " +
			"on both sides")
	}
}

// TestQueueDiff_ProviderLevelEnvironment_NoSpuriousReplace mirrors the
// Secret case for Queue, which also has no Update path.
func TestQueueDiff_ProviderLevelEnvironment_NoSpuriousReplace(t *testing.T) {
	s := newTestServer(t, "prod-env")

	diff, err := s.Diff(pgp.DiffRequest{
		Urn: modalUrn("Queue", "myqueue"),
		State: property.NewMap(map[string]property.Value{
			"name":    property.New("myqueue"),
			"queueId": property.New("q-123"),
		}),
		Inputs: property.NewMap(map[string]property.Value{
			"name":        property.New("myqueue"),
			"environment": property.New("prod-env"),
		}),
	})
	require.NoError(t, err)

	if diff.HasChanges {
		t.Errorf("expected no changes when resource-level environment matches the active provider-level "+
			"environment, got HasChanges=true with detailed diff: %+v", diff.DetailedDiff)
	}
	if _, ok := diff.DetailedDiff["environment"]; ok {
		t.Error("environment should not appear in DetailedDiff when it resolves to the same effective value " +
			"on both sides")
	}
}

// ---------------------------------------------------------------------------
// CheckConfig — environment variable fallbacks
// ---------------------------------------------------------------------------

// TestCheckConfig_EnvFallbacks verifies that unset provider config is filled
// from the MODAL_* environment variables, so the default provider works with
// no explicit modal:* config, and that explicit config still wins.
func TestCheckConfig_EnvFallbacks(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		envVar string
		config map[string]property.Value
		want   string
	}{
		{name: "token id from env", key: "tokenId", envVar: "MODAL_TOKEN_ID", want: "ak-from-env"},
		{name: "token secret from env", key: "tokenSecret", envVar: "MODAL_TOKEN_SECRET", want: "ak-from-env"},
		{name: "environment from env", key: "environment", envVar: "MODAL_ENVIRONMENT", want: "ak-from-env"},
		{name: "server addr from env", key: "serverAddr", envVar: "MODAL_SERVER_ADDR", want: "ak-from-env"},
		{
			name:   "explicit config wins over env",
			key:    "tokenId",
			envVar: "MODAL_TOKEN_ID",
			config: map[string]property.Value{"tokenId": property.New("ak-from-config")},
			want:   "ak-from-config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, "ak-from-env")

			p, err := New()
			require.NoError(t, err)
			s, err := integration.NewServer(t.Context(), "modal", semver.MustParse("0.0.1"), integration.WithProvider(p))
			require.NoError(t, err)

			resp, err := s.CheckConfig(pgp.CheckRequest{
				Urn:    resource.NewURN("stack", "proj", "", "pulumi:providers:modal", "default"),
				Inputs: property.NewMap(tt.config),
			})
			require.NoError(t, err)
			require.Empty(t, resp.Failures)

			got, ok := resp.Inputs.GetOk(tt.key)
			require.True(t, ok, "%s should be populated", tt.key)
			if got.Secret() {
				got = got.WithSecret(false)
			}
			require.Equal(t, tt.want, got.AsString())
		})
	}
}
