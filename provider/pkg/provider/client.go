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
	"net"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// defaultServerAddr is Modal's production gRPC control-plane endpoint.
const defaultServerAddr = "api.modal.com:443"

// providerClientVersion is the Modal client protocol version, sent as
// x-modal-client-version. It matches what the official Go SDK sends (it
// "behaves like" that Python SDK version) and is independent of the Pulumi
// provider's own version (pkg/version.Version).
const providerClientVersion = "1.0.0"

// modalSDKModule is the vendored Modal Go SDK, whose version the control plane
// expects in x-modal-libmodal-version.
const modalSDKModule = "github.com/modal-labs/modal-client/go"

// fallbackModalSDKVersion is used when build info is unavailable. A unit test
// keeps it equal to the version in go.mod, so bump both together when
// upgrading the Modal SDK.
const fallbackModalSDKVersion = "0.11.1"

// modalSDKVersion returns the vendored Modal Go SDK's version without the
// leading "v".
func modalSDKVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path != modalSDKModule {
				continue
			}
			if dep.Replace != nil && dep.Replace.Version != "" {
				dep = dep.Replace
			}
			if v := strings.TrimPrefix(dep.Version, "v"); v != "" && v != "(devel)" {
				return v
			}
		}
	}
	return fallbackModalSDKVersion
}

// libmodalVersionHeader is the x-modal-libmodal-version value. Modal's control
// plane validates it against the SDK versions it knows for the Go client type
// and fails requests like AppGetOrCreate with "Could not determine the SDK
// version for client type CLIENT_TYPE_LIBMODAL_GO" for anything else, so send
// what the official Go SDK sends: "modal-go/<sdk version>".
func libmodalVersionHeader() string { return "modal-go/" + modalSDKVersion() }

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
		return nil, fmt.Errorf("both tokenId and tokenSecret are required")
	}
	if serverAddr == "" {
		serverAddr = defaultServerAddr
	}

	creds := credentials.NewTLS(nil)
	if isLoopbackAddr(serverAddr) {
		// Plaintext is only ever used for a loopback address (a local mock
		// server in tests); every other endpoint, including the production
		// default, is always dialed over TLS.
		creds = insecure.NewCredentials()
	}
	conn, err := grpc.NewClient(
		serverAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(authInterceptor(tokenID, tokenSecret)),
		grpc.WithStreamInterceptor(streamAuthInterceptor(tokenID, tokenSecret)),
	)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", serverAddr, err)
	}

	return &modalClient{
		stub: pb.NewModalClientClient(conn),
		conn: conn,
	}, nil
}

// isLoopbackAddr reports whether addr (host:port) points at the local machine.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
			"x-modal-libmodal-version", libmodalVersionHeader(),
			"x-modal-token-id", tokenID,
			"x-modal-token-secret", tokenSecret,
		)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// streamAuthInterceptor injects the same headers on streaming RPCs, which the
// image build log stream needs.
func streamAuthInterceptor(tokenID, tokenSecret string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx,
			"x-modal-client-type", fmt.Sprintf("%d", clientTypeLibmodalGo),
			"x-modal-client-version", providerClientVersion,
			"x-modal-libmodal-version", libmodalVersionHeader(),
			"x-modal-token-id", tokenID,
			"x-modal-token-secret", tokenSecret,
		)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// notFoundOnImport reports a missing remote object during an import.
//
// The engine treats an empty ID from Read as "deleted" only on refresh; on import the
// framework accepts the empty response and an empty resource lands in state with exit
// code 0. An import is recognisable by Read arriving with neither prior inputs nor prior
// state, so there a missing object is an error. On refresh it returns nil and the caller
// returns the empty response that drops the resource from state.
func notFoundOnImport[A, S any](kind, id string, inputs A, state S) error {
	if reflect.ValueOf(inputs).IsZero() && reflect.ValueOf(state).IsZero() {
		return fmt.Errorf("cannot import %s %q: it does not exist (or the API key cannot see it); check the import ID", kind, id)
	}
	return nil
}
