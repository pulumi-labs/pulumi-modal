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
	"sort"
	"time"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This file extends the fake Modal control plane with the Proxy and
// ServiceUser services and the app layout RPC behind the AppDeployment resource.

type fakeProxy struct {
	id     string
	name   string
	env    string
	region string
	ips    []string
}

func (p *fakeProxy) proto() *pb.Proxy {
	var ips []*pb.ProxyIp
	for _, ip := range p.ips {
		ips = append(ips, pb.ProxyIp_builder{ProxyIp: ip, EnvironmentName: p.env}.Build())
	}
	return pb.Proxy_builder{ProxyId: p.id, Name: p.name, EnvironmentName: p.env, Region: p.region, ProxyIps: ips}.Build()
}

// newIP hands out a fresh documentation-range address. Callers hold f.mu.
func (f *fakeModal) newIP() string {
	f.seq++
	return fmt.Sprintf("203.0.113.%d", f.seq)
}

func (f *fakeModal) ProxyCreate(_ context.Context, req *pb.ProxyCreateRequest) (*pb.ProxyCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env := f.resolveEnv(req.GetEnvironmentName())
	for _, p := range f.proxies {
		if p.name == req.GetName() && p.env == env {
			return nil, status.Error(codes.AlreadyExists, "proxy already exists")
		}
	}
	region := req.GetRegion()
	if region == "" {
		region = "us-east"
	}
	p := &fakeProxy{id: f.nextID("pr"), name: req.GetName(), env: env, region: region}
	p.ips = []string{f.newIP()}
	f.proxies[p.id] = p
	return pb.ProxyCreateResponse_builder{Proxy: p.proto()}.Build(), nil
}

func (f *fakeModal) ProxyAddIp(_ context.Context, req *pb.ProxyAddIpRequest) (*pb.ProxyAddIpResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.proxies[req.GetProxyId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "proxy not found")
	}
	ip := f.newIP()
	p.ips = append(p.ips, ip)
	return pb.ProxyAddIpResponse_builder{ProxyIp: pb.ProxyIp_builder{ProxyIp: ip}.Build()}.Build(), nil
}

func (f *fakeModal) ProxyRemoveIp(_ context.Context, req *pb.ProxyRemoveIpRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.proxies {
		for i, ip := range p.ips {
			if ip == req.GetProxyIp() {
				p.ips = append(p.ips[:i], p.ips[i+1:]...)
				return &emptypb.Empty{}, nil
			}
		}
	}
	return nil, status.Error(codes.NotFound, "ip not found")
}

func (f *fakeModal) ProxyDelete(_ context.Context, req *pb.ProxyDeleteRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.proxies[req.GetProxyId()]; !ok {
		return nil, status.Error(codes.NotFound, "proxy not found")
	}
	delete(f.proxies, req.GetProxyId())
	return &emptypb.Empty{}, nil
}

func (f *fakeModal) ProxyList(context.Context, *emptypb.Empty) (*pb.ProxyListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.proxies))
	for id := range f.proxies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []*pb.Proxy
	for _, id := range ids {
		out = append(out, f.proxies[id].proto())
	}
	return pb.ProxyListResponse_builder{Proxies: out}.Build(), nil
}

// --- ServiceUser ---------------------------------------------------------------

type fakeServiceUser struct {
	id        string
	name      string
	tokenID   string
	scoped    bool
	expiresAt float64
}

func (u *fakeServiceUser) proto() *pb.ServiceUser {
	b := pb.ServiceUser_builder{ServiceUserId: u.id, Name: u.name, TokenId: u.tokenID, Scoped: u.scoped, Status: "active"}
	if u.expiresAt > 0 {
		b.ExpiresAt = &u.expiresAt
	}
	return b.Build()
}

func (f *fakeModal) ServiceUserCreate(_ context.Context, req *pb.ServiceUserCreateRequest) (*pb.ServiceUserCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.serviceUsers {
		if u.name == req.GetName() {
			return nil, status.Error(codes.AlreadyExists, "service user already exists")
		}
	}
	u := &fakeServiceUser{id: f.nextID("su"), name: req.GetName(), scoped: req.GetScoped()}
	u.tokenID = "ak-" + u.id
	b := pb.ServiceUserCreateResponse_builder{TokenId: u.tokenID, TokenSecret: "as-secret-" + u.id, ServiceUserId: u.id}
	if n := req.GetExpiresInSeconds(); n > 0 {
		u.expiresAt = 1893456000 + float64(n)
		b.ExpiresAt = timestamppb.New(time.Unix(int64(u.expiresAt), 0))
	}
	f.serviceUsers[u.id] = u
	return b.Build(), nil
}

func (f *fakeModal) ServiceUserDelete(_ context.Context, req *pb.ServiceUserDeleteRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.serviceUsers[req.GetServiceUserId()]; !ok {
		return nil, status.Error(codes.NotFound, "service user not found")
	}
	delete(f.serviceUsers, req.GetServiceUserId())
	return &emptypb.Empty{}, nil
}

func (f *fakeModal) ServiceUserInfo(_ context.Context, req *pb.ServiceUserInfoRequest) (*pb.ServiceUserInfoResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.serviceUsers {
		if u.name == req.GetServiceUserName() {
			return pb.ServiceUserInfoResponse_builder{ServiceUser: u.proto()}.Build(), nil
		}
	}
	return nil, status.Error(codes.NotFound, "service user not found")
}

func (f *fakeModal) ServiceUserList(context.Context, *emptypb.Empty) (*pb.ServiceUserListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.serviceUsers))
	for id := range f.serviceUsers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []*pb.ServiceUser
	for _, id := range ids {
		out = append(out, f.serviceUsers[id].proto())
	}
	return pb.ServiceUserListResponse_builder{ServiceUsers: out}.Build(), nil
}

// --- WebhookToken --------------------------------------------------------------

type fakeWebhookToken struct {
	id      string
	name    string
	scoped  bool
	created float64
}

func (t *fakeWebhookToken) proto() *pb.WebhookToken {
	return pb.WebhookToken_builder{TokenId: t.id, Name: t.name, Scoped: t.scoped, CreatedAt: t.created}.Build()
}

func (f *fakeModal) WebhookTokenCreate(_ context.Context, req *pb.WebhookTokenCreateRequest) (*pb.TokenCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	t := &fakeWebhookToken{id: "wk-" + f.nextID("t"), name: req.GetName(), scoped: req.GetScoped(), created: 1893456000}
	f.webhookTokens[t.id] = t
	return pb.TokenCreateResponse_builder{TokenId: t.id, TokenSecret: "ws-secret-" + t.id}.Build(), nil
}

func (f *fakeModal) WebhookTokenUpdate(_ context.Context, req *pb.WebhookTokenUpdateRequest) (*pb.WebhookToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.webhookTokens[req.GetTokenId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "webhook token not found")
	}
	if req.HasName() {
		t.name = req.GetName()
	}
	return t.proto(), nil
}

func (f *fakeModal) WebhookTokenDelete(_ context.Context, req *pb.TokenDeleteRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.webhookTokens[req.GetTokenId()]; !ok {
		return nil, status.Error(codes.NotFound, "webhook token not found")
	}
	delete(f.webhookTokens, req.GetTokenId())
	return &emptypb.Empty{}, nil
}

func (f *fakeModal) WebhookTokenList(context.Context, *emptypb.Empty) (*pb.WebhookTokenListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.webhookTokens))
	for id := range f.webhookTokens {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []*pb.WebhookToken
	for _, id := range ids {
		out = append(out, f.webhookTokens[id].proto())
	}
	return pb.WebhookTokenListResponse_builder{Tokens: out}.Build(), nil
}

// --- App layout ----------------------------------------------------------------

func (f *fakeModal) AppGetLayout(_ context.Context, req *pb.AppGetLayoutRequest) (*pb.AppGetLayoutResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failLayout {
		return nil, status.Error(codes.Unavailable, "layout unavailable")
	}
	a, ok := f.apps[req.GetAppId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "app not found")
	}
	ids := map[string]string{}
	for name := range a.functions {
		ids[name] = "fu-" + name
	}
	return pb.AppGetLayoutResponse_builder{AppLayout: pb.AppLayout_builder{FunctionIds: ids}.Build()}.Build(), nil
}
