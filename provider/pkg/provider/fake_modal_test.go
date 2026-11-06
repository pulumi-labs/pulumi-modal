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
	"net"
	"regexp"
	"strings"
	"sync"
	"testing"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeObject is one Secret, Volume or Queue held by the fake Modal control
// plane.
type fakeObject struct {
	id      string
	name    string
	env     string
	version pb.VolumeFsVersion
	values  map[string]string
}

// fakeModal is an in-memory implementation of the slice of Modal's gRPC API
// the provider uses. It enforces the static token headers on every call and
// mirrors the semantics the provider depends on: names are unique per
// environment, CREATE_FAIL_IF_EXISTS fails on a duplicate, and lookups by name
// need the environment while lookups by ID do not.
type fakeModal struct {
	pb.UnimplementedModalClientServer

	mu         sync.Mutex
	seq        int
	defaultEnv string
	secrets    map[string]*fakeObject
	volumes    map[string]*fakeObject
	queues     map[string]*fakeObject
	dicts      map[string]*fakeObject
	envListed  int

	envs   map[string]*fakeEnv
	apps   map[string]*fakeApp
	images map[string]*fakeImage
	tags   map[string]string // image tag (env/name:tag) -> image id

	proxies       map[string]*fakeProxy
	serviceUsers  map[string]*fakeServiceUser
	webhookTokens map[string]*fakeWebhookToken

	// Failure injection.
	failPublish bool
	failSetTags bool
	// failAppLookup and failLayout break the read-back after a deploy.
	failAppLookup bool
	failLayout    bool
}

func newFakeModal() *fakeModal {
	return &fakeModal{
		defaultEnv: "main",
		secrets:    map[string]*fakeObject{},
		volumes:    map[string]*fakeObject{},
		queues:     map[string]*fakeObject{},
		dicts:      map[string]*fakeObject{},
		envs: map[string]*fakeEnv{
			"env-main":  {id: "env-main", name: "main", isDefault: true, suffix: "main-suffix"},
			"env-other": {id: "env-other", name: "other", suffix: "other-suffix"},
		},
		apps:   map[string]*fakeApp{},
		images: map[string]*fakeImage{},
		tags:   map[string]string{},

		proxies:       map[string]*fakeProxy{},
		serviceUsers:  map[string]*fakeServiceUser{},
		webhookTokens: map[string]*fakeWebhookToken{},
	}
}

// knownLibmodalVersion is the form of x-modal-libmodal-version the live
// control plane accepts for the Go client type.
var knownLibmodalVersion = regexp.MustCompile(`^modal-go/[0-9]+[.][0-9]+[.][0-9]+$`)

// startFakeModal serves a fakeModal on a loopback port and returns it with
// its address.
func startFakeModal(t *testing.T) (*fakeModal, string) {
	t.Helper()
	f := newFakeModal()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(grpc.UnaryInterceptor(func(
		ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		if got := md.Get("x-modal-token-id"); len(got) != 1 || got[0] != "ak-test" {
			return nil, status.Error(codes.Unauthenticated, "bad token id")
		}
		if got := md.Get("x-modal-token-secret"); len(got) != 1 || got[0] != "as-test" {
			return nil, status.Error(codes.Unauthenticated, "bad token secret")
		}
		// Like the live control plane, reject clients it cannot place: the
		// official Go SDK sends client type 9, client version 1.0.0 and
		// "modal-go/<sdk version>".
		if info.FullMethod == "/modal.client.ModalClient/AppGetOrCreate" {
			if got := md.Get("x-modal-client-type"); len(got) != 1 || got[0] != "9" {
				return nil, status.Error(codes.FailedPrecondition, "Could not determine the SDK version for client type "+strings.Join(got, ","))
			}
			if got := md.Get("x-modal-client-version"); len(got) != 1 || got[0] != "1.0.0" {
				return nil, status.Error(codes.FailedPrecondition, "Unsupported client version")
			}
			if got := md.Get("x-modal-libmodal-version"); len(got) != 1 || !knownLibmodalVersion.MatchString(got[0]) {
				return nil, status.Error(codes.FailedPrecondition, "Could not determine the SDK version for client type CLIENT_TYPE_LIBMODAL_GO")
			}
		}
		return handler(ctx, req)
	}))
	pb.RegisterModalClientServer(srv, f)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return f, lis.Addr().String()
}

func (f *fakeModal) resolveEnv(env string) string {
	if env == "" {
		return f.defaultEnv
	}
	return env
}

func (f *fakeModal) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

func find(m map[string]*fakeObject, name, env string) *fakeObject {
	for _, o := range m {
		if o.name == name && o.env == env {
			return o
		}
	}
	return nil
}

// getOrCreate implements the shared get-or-create semantics.
func (f *fakeModal) getOrCreate(m map[string]*fakeObject, prefix, name, env string,
	creation pb.ObjectCreationType, mk func(*fakeObject),
) (*fakeObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env = f.resolveEnv(env)
	existing := find(m, name, env)
	switch creation {
	case pb.ObjectCreationType_OBJECT_CREATION_TYPE_CREATE_FAIL_IF_EXISTS:
		if existing != nil {
			return nil, status.Error(codes.AlreadyExists, "already exists")
		}
	case pb.ObjectCreationType_OBJECT_CREATION_TYPE_UNSPECIFIED:
		if existing == nil {
			return nil, status.Error(codes.NotFound, "not found")
		}
		return existing, nil
	}
	if existing != nil {
		return existing, nil
	}
	o := &fakeObject{id: f.nextID(prefix), name: name, env: env}
	if mk != nil {
		mk(o)
	}
	m[o.id] = o
	return o, nil
}

func (f *fakeModal) get(m map[string]*fakeObject, id string) (*fakeObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := m[id]
	if !ok {
		return nil, status.Error(codes.NotFound, "not found")
	}
	cp := *o
	return &cp, nil
}

func (f *fakeModal) remove(m map[string]*fakeObject, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := m[id]; !ok {
		return status.Error(codes.NotFound, "not found")
	}
	delete(m, id)
	return nil
}

func (f *fakeModal) SecretGetOrCreate(_ context.Context, req *pb.SecretGetOrCreateRequest) (*pb.SecretGetOrCreateResponse, error) {
	o, err := f.getOrCreate(f.secrets, "st", req.GetDeploymentName(), req.GetEnvironmentName(), req.GetObjectCreationType(),
		func(o *fakeObject) {
			o.values = map[string]string{}
			for k, v := range req.GetEnvDict() {
				o.values[k] = v
			}
		})
	if err != nil {
		return nil, err
	}
	return pb.SecretGetOrCreateResponse_builder{SecretId: o.id}.Build(), nil
}

func (f *fakeModal) SecretGetInfo(_ context.Context, req *pb.SecretGetInfoRequest) (*pb.SecretGetInfoResponse, error) {
	o, err := f.get(f.secrets, req.GetSecretId())
	if err != nil {
		return nil, err
	}
	keys := []string{}
	for k := range o.values {
		keys = append(keys, k)
	}
	return pb.SecretGetInfoResponse_builder{Metadata: pb.SecretMetadata_builder{
		Name: o.name, Keys: keys, EnvironmentName: o.env,
	}.Build()}.Build(), nil
}

func (f *fakeModal) SecretUpdate(_ context.Context, req *pb.SecretUpdateRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.secrets[req.GetSecretId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "not found")
	}
	for _, u := range req.GetUpdates() {
		if u.HasValue() {
			o.values[u.GetKey()] = u.GetValue()
		} else {
			delete(o.values, u.GetKey())
		}
	}
	return &emptypb.Empty{}, nil
}

func (f *fakeModal) SecretDelete(_ context.Context, req *pb.SecretDeleteRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, f.remove(f.secrets, req.GetSecretId())
}

func (f *fakeModal) VolumeGetOrCreate(_ context.Context, req *pb.VolumeGetOrCreateRequest) (*pb.VolumeGetOrCreateResponse, error) {
	o, err := f.getOrCreate(f.volumes, "vo", req.GetDeploymentName(), req.GetEnvironmentName(), req.GetObjectCreationType(),
		func(o *fakeObject) { o.version = req.GetVersion() })
	if err != nil {
		return nil, err
	}
	return pb.VolumeGetOrCreateResponse_builder{VolumeId: o.id, Version: o.version}.Build(), nil
}

func (f *fakeModal) VolumeGetById(_ context.Context, req *pb.VolumeGetByIdRequest) (*pb.VolumeGetByIdResponse, error) {
	o, err := f.get(f.volumes, req.GetVolumeId())
	if err != nil {
		return nil, err
	}
	return pb.VolumeGetByIdResponse_builder{VolumeId: o.id, Metadata: pb.VolumeMetadata_builder{
		Name: o.name, Version: o.version,
	}.Build()}.Build(), nil
}

func (f *fakeModal) VolumeRename(_ context.Context, req *pb.VolumeRenameRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.volumes[req.GetVolumeId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "not found")
	}
	if find(f.volumes, req.GetName(), o.env) != nil {
		return nil, status.Error(codes.AlreadyExists, "name taken")
	}
	o.name = req.GetName()
	return &emptypb.Empty{}, nil
}

func (f *fakeModal) VolumeDelete(_ context.Context, req *pb.VolumeDeleteRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, f.remove(f.volumes, req.GetVolumeId())
}

func (f *fakeModal) QueueGetOrCreate(_ context.Context, req *pb.QueueGetOrCreateRequest) (*pb.QueueGetOrCreateResponse, error) {
	o, err := f.getOrCreate(f.queues, "qu", req.GetDeploymentName(), req.GetEnvironmentName(), req.GetObjectCreationType(), nil)
	if err != nil {
		return nil, err
	}
	return pb.QueueGetOrCreateResponse_builder{QueueId: o.id}.Build(), nil
}

func (f *fakeModal) QueueGetById(_ context.Context, req *pb.QueueGetByIdRequest) (*pb.QueueGetByIdResponse, error) {
	o, err := f.get(f.queues, req.GetQueueId())
	if err != nil {
		return nil, err
	}
	return pb.QueueGetByIdResponse_builder{QueueId: o.id, Metadata: pb.QueueMetadata_builder{Name: o.name}.Build()}.Build(), nil
}

func (f *fakeModal) QueueDelete(_ context.Context, req *pb.QueueDeleteRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, f.remove(f.queues, req.GetQueueId())
}
