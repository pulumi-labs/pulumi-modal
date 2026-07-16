// Package provider implements the Modal Pulumi provider.
//
// Modal (modal.com) is a serverless GPU/CPU compute platform. It does not
// publish a Terraform provider, so this package talks directly to Modal's
// public gRPC control-plane API using the generated protobuf client that
// Modal itself publishes at github.com/modal-labs/modal-client/go/proto/modal_proto.
//
// Authentication mirrors the Modal Python SDK exactly: every RPC carries the
// static x-modal-token-id / x-modal-token-secret headers (see
// modal-client/py/modal/client.py, AUTH_METADATA). We deliberately do not
// replicate the Go SDK's additional proactive x-modal-auth-token exchange
// (AuthTokenManager) — that flow is a session-token optimization layered on
// top of the same static-header authentication the Python SDK, the CLI, and
// the server itself accept as sufficient on their own. Modal's own docs
// disclaim the gRPC surface as "not a public API" in the stability sense
// (methods can be added/changed), but the wire-level authentication contract
// (static bearer-style headers) is stable across every official SDK and is
// what this provider relies on.
//
// Resources:
//   - Secret — a durable, named collection of key/value pairs injected as
//     environment variables into Modal Functions.
//   - Volume — a durable, named distributed filesystem volume.
//   - Queue  — a durable, named FIFO/LIFO queue for passing data between Functions.
package provider

import (
	"context"
	"fmt"
	"regexp"
	"time"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// defaultServerAddr is Modal's production gRPC control-plane endpoint.
const defaultServerAddr = "api.modal.com:443"

// providerClientVersion identifies this provider to Modal's control plane.
// It is deliberately independent from the Pulumi provider's own semantic
// version (pkg/version.Version) since it describes wire-protocol
// compatibility, not provider release cadence.
const providerClientVersion = "1.0.0"

// clientTypeLibmodalGo is pb.ClientType_CLIENT_TYPE_LIBMODAL_GO. This provider
// is a Go program built directly on Modal's published Go protobuf client, so
// it identifies honestly as a Go libmodal client rather than impersonating
// another SDK.
const clientTypeLibmodalGo = 9

// envVarNameRegex mirrors Modal's own server-side validation for Secret keys:
// letters, numbers, and underscores, not starting with a digit.
var envVarNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func validateEnvVarName(key string) error {
	if !envVarNameRegex.MatchString(key) {
		return fmt.Errorf(
			"modal: secret key %q is invalid; only letters, numbers, and underscores are allowed, and the name cannot start with a digit",
			key,
		)
	}
	return nil
}

// modalClient wraps the generated gRPC stub with credential injection and a
// closeable connection.
type modalClient struct {
	stub pb.ModalClientClient
	conn *grpc.ClientConn
}

// newModalClient dials Modal's control plane and returns a client that
// injects the given token on every RPC. serverAddr may be overridden (e.g.
// for tests); pass "" to use the production endpoint.
func newModalClient(tokenID, tokenSecret, serverAddr string) (*modalClient, error) {
	if tokenID == "" || tokenSecret == "" {
		return nil, fmt.Errorf("modal: both tokenId and tokenSecret are required")
	}
	if serverAddr == "" {
		serverAddr = defaultServerAddr
	}

	creds := credentials.NewTLS(nil)
	conn, err := grpc.NewClient(
		serverAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(authInterceptor(tokenID, tokenSecret)),
	)
	if err != nil {
		return nil, fmt.Errorf("modal: dial %s: %w", serverAddr, err)
	}

	return &modalClient{
		stub: pb.NewModalClientClient(conn),
		conn: conn,
	}, nil
}

func (c *modalClient) Close() error {
	return c.conn.Close()
}

// authInterceptor injects the static token headers every official Modal SDK
// sends on each unary RPC.
func authInterceptor(tokenID, tokenSecret string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx,
			"x-modal-client-type", fmt.Sprintf("%d", clientTypeLibmodalGo),
			"x-modal-client-version", providerClientVersion,
			"x-modal-libmodal-version", "pulumi-modal/"+providerClientVersion,
			"x-modal-token-id", tokenID,
			"x-modal-token-secret", tokenSecret,
		)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}
