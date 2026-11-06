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

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ---------------------------------------------------------------------------
// Proxy resource
// ---------------------------------------------------------------------------

// Proxy provisions a Modal Proxy: a set of static outbound IP addresses that
// Functions can route their egress traffic through (for IP allow-lists).
type Proxy struct{}

// ProxyArgs are the user-supplied inputs for a Modal Proxy.
type ProxyArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the proxy's unique name within its environment. Changing this
	// forces a new resource.
	Name string `pulumi:"name"`
	// Environment is the Modal environment the proxy lives in. Changing this
	// forces a new resource. Falls back to the provider-level environment.
	Environment *string `pulumi:"environment,optional"`
	// Region is the region the proxy's IP addresses are allocated in. When
	// unset Modal picks one. Changing this forces a new resource.
	Region *string `pulumi:"region,optional"`
	// IpCount is the number of static IP addresses to keep on the proxy. When
	// unset the addresses Modal allocated at creation are left alone.
	IpCount *int `pulumi:"ipCount,optional"`
}

// ProxyState holds the computed outputs for a Modal Proxy.
type ProxyState struct {
	ProxyArgs

	// ProxyId is the Modal-assigned unique identifier for the proxy.
	ProxyId string `pulumi:"proxyId"`
	// Ips are the proxy's static IP addresses, sorted.
	Ips []string `pulumi:"ips"`
}

func (r *Proxy) Annotate(a infer.Annotator) {
	a.Describe(&r, "A Modal Proxy: static outbound IP addresses for Modal Functions, for services that allow-list by source IP. "+
		"Static IPs are a paid Modal plan feature. Adjust ipCount to add or release addresses in place. "+
		"See https://modal.com/docs/guide/proxy-ips.")
}

func (a *ProxyArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The proxy's unique name within its environment. Changing this forces a new resource.")
	ann.Describe(&a.Environment, "The Modal environment the proxy lives in. Changing this forces a new resource. "+
		"When unset, the provider-level environment or the account's default environment is used.")
	ann.Describe(&a.Region, "The region the proxy's IP addresses are allocated in. When unset Modal picks one. "+
		"Changing this forces a new resource.")
	ann.Describe(&a.IpCount, "The number of static IP addresses to keep on the proxy. Raising it allocates addresses and "+
		"lowering it releases the most recently listed ones, in place. When unset the addresses allocated at creation are left alone.")
}

func (s *ProxyState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.ProxyId, "The Modal-assigned identifier of the proxy.")
	ann.Describe(&s.Ips, "The proxy's static IP addresses, sorted.")
}

var _ infer.CustomResource[ProxyArgs, ProxyState] = (*Proxy)(nil)
var _ infer.CustomDiff[ProxyArgs, ProxyState] = (*Proxy)(nil)
var _ infer.CustomUpdate[ProxyArgs, ProxyState] = (*Proxy)(nil)
var _ infer.CustomRead[ProxyArgs, ProxyState] = (*Proxy)(nil)
var _ infer.CustomDelete[ProxyState] = (*Proxy)(nil)

// Diff replaces on a name, environment or region change (Modal has no rename
// or move call) and updates in place when only ipCount changed.
func (r *Proxy) Diff(ctx context.Context, req infer.DiffRequest[ProxyArgs, ProxyState]) (infer.DiffResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	if req.Inputs.Name != req.State.Name {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	if environmentChanged(req.Inputs.Environment, req.State.Environment, withResourceTokens(cfg, req.Inputs.TokenID, req.Inputs.TokenSecret)) {
		resp.DetailedDiff["environment"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	if req.Inputs.Region != nil && *req.Inputs.Region != derefStr(req.State.Region) {
		resp.DetailedDiff["region"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	if req.Inputs.IpCount != nil && *req.Inputs.IpCount != len(req.State.Ips) {
		resp.DetailedDiff["ipCount"] = pgp.PropertyDiff{Kind: pgp.Update}
	}
	resp.HasChanges = len(resp.DetailedDiff) > 0
	return resp, nil
}

func proxyIPs(p *pb.Proxy) []string {
	ips := []string{}
	for _, ip := range p.GetProxyIps() {
		ips = append(ips, ip.GetProxyIp())
	}
	sort.Strings(ips)
	return ips
}

func (r *Proxy) Create(ctx context.Context, req infer.CreateRequest[ProxyArgs]) (infer.CreateResponse[ProxyState], error) {
	args := req.Inputs
	if args.IpCount != nil && *args.IpCount < 1 {
		return infer.CreateResponse[ProxyState]{}, fmt.Errorf("modal: ipCount must be at least 1")
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		state := ProxyState{ProxyArgs: args}
		if env := resolveEnv(args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret)); env != "" {
			state.Environment = &env
		}
		return infer.CreateResponse[ProxyState]{ID: "", Output: state}, nil
	}

	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[ProxyState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	environment := effectiveEnvironment(rpcCtx, client, args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret))
	created, err := client.stub.ProxyCreate(rpcCtx, pb.ProxyCreateRequest_builder{
		Name:            args.Name,
		EnvironmentName: environment,
		Region:          derefStr(args.Region),
	}.Build())
	if err != nil {
		return infer.CreateResponse[ProxyState]{}, fmt.Errorf("modal: create proxy %q: %w", args.Name, err)
	}
	p := created.GetProxy()
	id := p.GetProxyId()
	ips := proxyIPs(p)

	if args.IpCount != nil {
		ips, err = resizeProxy(rpcCtx, client, id, ips, *args.IpCount)
		if err != nil {
			// Keep the proxy in state so a retry resizes it instead of
			// leaking the allocated addresses.
			return infer.CreateResponse[ProxyState]{ID: id, Output: ProxyState{ProxyArgs: args, ProxyId: id, Ips: ips}},
				infer.ResourceInitFailedError{Reasons: []string{err.Error()}}
		}
	}

	state := ProxyState{ProxyArgs: args, ProxyId: id, Ips: ips}
	if environment != "" {
		state.Environment = &environment
	}
	if state.Region == nil && p.GetRegion() != "" {
		region := p.GetRegion()
		state.Region = &region
	}
	return infer.CreateResponse[ProxyState]{ID: id, Output: state}, nil
}

// resizeProxy adds or releases IP addresses until the proxy has want of them,
// and returns the resulting sorted address list.
func resizeProxy(ctx context.Context, client *modalClient, id string, ips []string, want int) ([]string, error) {
	ips = append([]string(nil), ips...)
	for len(ips) < want {
		added, err := client.stub.ProxyAddIp(ctx, pb.ProxyAddIpRequest_builder{ProxyId: id}.Build())
		if err != nil {
			sort.Strings(ips)
			return ips, fmt.Errorf("modal: add an IP to proxy %q: %w", id, err)
		}
		ips = append(ips, added.GetProxyIp().GetProxyIp())
	}
	sort.Strings(ips)
	for len(ips) > want {
		last := ips[len(ips)-1]
		if _, err := client.stub.ProxyRemoveIp(ctx, pb.ProxyRemoveIpRequest_builder{ProxyIp: last}.Build()); err != nil {
			return ips, fmt.Errorf("modal: release IP %s of proxy %q: %w", last, id, err)
		}
		ips = ips[:len(ips)-1]
	}
	return ips, nil
}

// findProxy finds a proxy by ID in the workspace listing; nil means it no
// longer exists.
func findProxy(ctx context.Context, client *modalClient, id string) (*pb.Proxy, error) {
	list, err := client.stub.ProxyList(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	for _, p := range list.GetProxies() {
		if p.GetProxyId() == id {
			return p, nil
		}
	}
	return nil, nil
}

func (r *Proxy) Update(ctx context.Context, req infer.UpdateRequest[ProxyArgs, ProxyState]) (infer.UpdateResponse[ProxyState], error) {
	state := req.State
	state.ProxyArgs = req.Inputs
	state.Environment = req.State.Environment
	if state.Region == nil {
		state.Region = req.State.Region
	}
	if req.DryRun || req.Inputs.IpCount == nil {
		return infer.UpdateResponse[ProxyState]{Output: state}, nil
	}
	if *req.Inputs.IpCount < 1 {
		return infer.UpdateResponse[ProxyState]{}, fmt.Errorf("modal: ipCount must be at least 1")
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.Inputs.TokenID, req.Inputs.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[ProxyState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	ips, err := resizeProxy(rpcCtx, client, req.ID, req.State.Ips, *req.Inputs.IpCount)
	state.Ips = ips
	if err != nil {
		return infer.UpdateResponse[ProxyState]{Output: state}, infer.ResourceInitFailedError{Reasons: []string{err.Error()}}
	}
	return infer.UpdateResponse[ProxyState]{Output: state}, nil
}

// Read refreshes the proxy by ID (through the workspace listing, since Modal
// has no get-by-ID call), so it works for refresh and import from the ID alone.
func (r *Proxy) Read(ctx context.Context, req infer.ReadRequest[ProxyArgs, ProxyState]) (infer.ReadResponse[ProxyArgs, ProxyState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[ProxyArgs, ProxyState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	p, err := findProxy(rpcCtx, client, req.ID)
	if err != nil {
		return empty, fmt.Errorf("modal: read proxy %q: %w", req.ID, err)
	}
	if p == nil {
		return empty, nil
	}

	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	state.ProxyId = req.ID
	state.Name = p.GetName()
	state.Ips = proxyIPs(p)
	if env := p.GetEnvironmentName(); env != "" {
		state.Environment = &env
	}
	if region := p.GetRegion(); region != "" {
		state.Region = &region
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = state.Name
	if state.IpCount != nil {
		// Surface addresses added or released outside Pulumi as drift.
		n := len(state.Ips)
		state.IpCount = &n
		inputs.IpCount = &n
	}
	if importing {
		inputs.Environment = state.Environment
		inputs.Region = state.Region
	} else if inputs.Region != nil {
		inputs.Region = state.Region
	}
	return infer.ReadResponse[ProxyArgs, ProxyState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

// Delete removes the Proxy and releases its IP addresses.
func (r *Proxy) Delete(ctx context.Context, req infer.DeleteRequest[ProxyState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	_, err = client.stub.ProxyDelete(rpcCtx, pb.ProxyDeleteRequest_builder{ProxyId: req.ID}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: delete proxy %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}
