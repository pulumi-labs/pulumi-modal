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
	"strings"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ---------------------------------------------------------------------------
// Domain resource
// ---------------------------------------------------------------------------

// Domain registers a custom domain with the Modal workspace so web endpoints
// can be served from it.
type Domain struct{}

// DomainArgs are the user-supplied inputs for a Modal custom domain.
type DomainArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// DomainName is the fully qualified domain name to register. Changing this
	// forces a new resource.
	DomainName string `pulumi:"domainName"`
	// Verify asks Modal to check the DNS records and issue the certificate.
	Verify *bool `pulumi:"verify,optional"`
}

// DomainDNSRecord is a DNS record the domain owner must create.
type DomainDNSRecord struct {
	Type  string `pulumi:"type"`
	Name  string `pulumi:"name"`
	Value string `pulumi:"value"`
}

func (r *DomainDNSRecord) Annotate(ann infer.Annotator) {
	ann.Describe(&r.Type, "The DNS record type: a, txt or cname.")
	ann.Describe(&r.Name, "The record's name (host).")
	ann.Describe(&r.Value, "The record's value.")
}

// DomainState holds the computed outputs for a Modal custom domain.
type DomainState struct {
	DomainArgs

	// DomainId is the Modal-assigned unique identifier for the domain.
	DomainId string `pulumi:"domainId"`
	// CreatedAt is when the domain was registered, as an RFC 3339 timestamp.
	CreatedAt *string `pulumi:"createdAt,optional"`
	// CertificateStatus is pending, issued, failed or revoked.
	CertificateStatus string `pulumi:"certificateStatus"`
	// DnsRecords are the DNS records to create at the domain's DNS provider.
	DnsRecords []DomainDNSRecord `pulumi:"dnsRecords"`
	// Verified is true once the certificate has been issued.
	Verified bool `pulumi:"verified"`
}

func (r *Domain) Annotate(a infer.Annotator) {
	a.Describe(&r, "A custom domain registered with the Modal workspace. Create the DNS records listed in dnsRecords at your DNS provider, "+
		"then set verify to true so Modal checks them and issues the certificate. "+
		"Modal has no call that deletes a domain: destroying this resource only removes it from Pulumi state and the domain stays "+
		"registered in Modal (remove it in the Modal dashboard or through Modal support). Creating a domain that is already registered "+
		"adopts it. See https://modal.com/docs/guide/custom-domain.")
}

func (a *DomainArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.DomainName, "The fully qualified domain name to register, such as `api.example.com`. Changing this forces a new resource.")
	ann.Describe(&a.Verify, "When true, Modal checks the domain's DNS records and issues its certificate. The check runs on create, update and "+
		"refresh until the certificate is issued. Defaults to false.")
}

func (s *DomainState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.DomainId, "The Modal-assigned identifier of the domain.")
	ann.Describe(&s.CreatedAt, "When the domain was registered, as an RFC 3339 timestamp.")
	ann.Describe(&s.CertificateStatus, "The certificate status: `pending`, `issued`, `failed` or `revoked`.")
	ann.Describe(&s.DnsRecords, "The DNS records to create at the domain's DNS provider so Modal can verify ownership and route traffic.")
	ann.Describe(&s.Verified, "True once the domain's certificate has been issued.")
}

var _ infer.CustomResource[DomainArgs, DomainState] = (*Domain)(nil)
var _ infer.CustomDiff[DomainArgs, DomainState] = (*Domain)(nil)
var _ infer.CustomUpdate[DomainArgs, DomainState] = (*Domain)(nil)
var _ infer.CustomRead[DomainArgs, DomainState] = (*Domain)(nil)
var _ infer.CustomDelete[DomainState] = (*Domain)(nil)

// Diff replaces on a domain name change and updates in place when only verify changed.
func (r *Domain) Diff(_ context.Context, req infer.DiffRequest[DomainArgs, DomainState]) (infer.DiffResponse, error) {
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	if !strings.EqualFold(req.Inputs.DomainName, req.State.DomainName) {
		resp.DetailedDiff["domainName"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	if boolOr(req.Inputs.Verify) != boolOr(req.State.Verify) {
		resp.DetailedDiff["verify"] = pgp.PropertyDiff{Kind: pgp.Update}
	}
	resp.HasChanges = len(resp.DetailedDiff) > 0
	return resp, nil
}

func certificateStatusName(s pb.CertificateStatus) string {
	switch s {
	case pb.CertificateStatus_CERTIFICATE_STATUS_ISSUED:
		return "issued"
	case pb.CertificateStatus_CERTIFICATE_STATUS_FAILED:
		return "failed"
	case pb.CertificateStatus_CERTIFICATE_STATUS_REVOKED:
		return "revoked"
	default:
		return "pending"
	}
}

func dnsRecordTypeName(t pb.DNSRecordType) string {
	switch t {
	case pb.DNSRecordType_DNS_RECORD_TYPE_TXT:
		return "txt"
	case pb.DNSRecordType_DNS_RECORD_TYPE_CNAME:
		return "cname"
	default:
		return "a"
	}
}

// applyDomain copies a Modal domain into state, keeping the inputs.
func applyDomain(state *DomainState, d *pb.Domain) {
	state.DomainId = d.GetDomainId()
	state.DomainName = d.GetDomainName()
	state.CertificateStatus = certificateStatusName(d.GetCertificateStatus())
	state.Verified = d.GetCertificateStatus() == pb.CertificateStatus_CERTIFICATE_STATUS_ISSUED
	state.DnsRecords = []DomainDNSRecord{}
	for _, rec := range d.GetDnsRecords() {
		state.DnsRecords = append(state.DnsRecords, DomainDNSRecord{
			Type: dnsRecordTypeName(rec.GetType()), Name: rec.GetName(), Value: rec.GetValue(),
		})
	}
	if d.GetCreatedAt() > 0 {
		s := unixToRFC3339(d.GetCreatedAt())
		state.CreatedAt = &s
	}
}

// listDomain finds a domain in the workspace listing by ID (when id is set) or
// by name; nil means there is none.
func listDomain(ctx context.Context, client *modalClient, id, name string) (*pb.Domain, error) {
	list, err := client.stub.DomainList(ctx, pb.DomainListRequest_builder{}.Build())
	if err != nil {
		return nil, err
	}
	for _, d := range list.GetDomains() {
		if (id != "" && d.GetDomainId() == id) || (id == "" && strings.EqualFold(d.GetDomainName(), name)) {
			return d, nil
		}
	}
	return nil, nil
}

// verifyDomain asks Modal to check the DNS records and returns the refreshed domain.
func verifyDomain(ctx context.Context, client *modalClient, id string) (*pb.Domain, error) {
	got, err := client.stub.DomainCertificateVerify(ctx, pb.DomainCertificateVerifyRequest_builder{DomainId: id}.Build())
	if err != nil {
		return nil, err
	}
	return got.GetDomain(), nil
}

func (r *Domain) Create(ctx context.Context, req infer.CreateRequest[DomainArgs]) (infer.CreateResponse[DomainState], error) {
	args := req.Inputs
	if strings.TrimSpace(args.DomainName) == "" {
		return infer.CreateResponse[DomainState]{}, fmt.Errorf("modal: domainName must not be empty")
	}
	if req.DryRun {
		return infer.CreateResponse[DomainState]{Output: DomainState{DomainArgs: args}}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[DomainState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	// Modal cannot delete a domain, so a destroy followed by an up finds the
	// domain still registered: adopt it rather than fail on the duplicate.
	existing, err := listDomain(rpcCtx, client, "", args.DomainName)
	if err != nil {
		return infer.CreateResponse[DomainState]{}, fmt.Errorf("modal: list domains: %w", err)
	}
	if existing != nil {
		pgp.GetLogger(ctx).Warningf("modal: domain %q is already registered (%s); adopting it", args.DomainName, existing.GetDomainId())
	} else {
		created, err := client.stub.DomainCreate(rpcCtx, pb.DomainCreateRequest_builder{DomainName: args.DomainName}.Build())
		if err != nil {
			return infer.CreateResponse[DomainState]{}, fmt.Errorf("modal: create domain %q: %w", args.DomainName, err)
		}
		existing, err = listDomain(rpcCtx, client, created.GetDomainId(), "")
		if err != nil || existing == nil {
			// The domain exists; fall back to what the create call returned.
			existing = pb.Domain_builder{
				DomainId: created.GetDomainId(), DomainName: args.DomainName, DnsRecords: created.GetDnsRecords(),
			}.Build()
		}
	}
	state := DomainState{DomainArgs: args}
	applyDomain(&state, existing)
	state.DomainName = args.DomainName
	if boolOr(args.Verify) && !state.Verified {
		if v, err := verifyDomain(rpcCtx, client, state.DomainId); err != nil {
			// Keep the domain in state so a retry verifies it.
			return infer.CreateResponse[DomainState]{ID: state.DomainId, Output: state},
				infer.ResourceInitFailedError{Reasons: []string{fmt.Sprintf("verify domain %q: %v", args.DomainName, err)}}
		} else {
			applyDomain(&state, v)
			state.DomainName = args.DomainName
		}
	}
	return infer.CreateResponse[DomainState]{ID: state.DomainId, Output: state}, nil
}

// Update only verifies in place.
func (r *Domain) Update(ctx context.Context, req infer.UpdateRequest[DomainArgs, DomainState]) (infer.UpdateResponse[DomainState], error) {
	state := req.State
	state.DomainArgs = req.Inputs
	if req.DryRun || !boolOr(req.Inputs.Verify) || state.Verified {
		return infer.UpdateResponse[DomainState]{Output: state}, nil
	}
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.Inputs.TokenID, req.Inputs.TokenSecret)
	if err != nil {
		return infer.UpdateResponse[DomainState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()
	rpcCtx, cancel := callCtx()
	defer cancel()
	v, err := verifyDomain(rpcCtx, client, req.ID)
	if err != nil {
		return infer.UpdateResponse[DomainState]{Output: state},
			infer.ResourceInitFailedError{Reasons: []string{fmt.Sprintf("verify domain %q: %v", state.DomainName, err)}}
	}
	applyDomain(&state, v)
	state.DomainName = req.Inputs.DomainName
	return infer.UpdateResponse[DomainState]{Output: state}, nil
}

// Read refreshes the domain by ID through the workspace listing (Modal has no
// get-by-ID call), so it works for refresh and import from the ID alone.
func (r *Domain) Read(ctx context.Context, req infer.ReadRequest[DomainArgs, DomainState]) (infer.ReadResponse[DomainArgs, DomainState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[DomainArgs, DomainState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	d, err := listDomain(rpcCtx, client, req.ID, "")
	if err != nil {
		return empty, fmt.Errorf("modal: read domain %q: %w", req.ID, err)
	}
	if d == nil {
		if err := notFoundOnImport("domain", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}
	state := req.State
	applyDomain(&state, d)
	if boolOr(state.Verify) && !state.Verified {
		// A failed check keeps the values just read.
		if v, verr := verifyDomain(rpcCtx, client, req.ID); verr == nil && v != nil {
			applyDomain(&state, v)
		}
	}
	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.DomainName = state.DomainName
	return infer.ReadResponse[DomainArgs, DomainState]{ID: req.ID, Inputs: inputs, State: state}, nil
}

// Delete only forgets the domain: Modal has no call that removes one.
func (r *Domain) Delete(context.Context, infer.DeleteRequest[DomainState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, nil
}

// ---------------------------------------------------------------------------
// getDomain function
// ---------------------------------------------------------------------------

// GetDomain looks up a custom domain registered with the workspace.
type GetDomain struct{}

// GetDomainArgs selects a domain by name or ID.
type GetDomainArgs struct {
	// DomainName is the domain's fully qualified name.
	DomainName *string `pulumi:"domainName,optional"`
	// DomainId is the Modal-assigned domain ID.
	DomainId *string `pulumi:"domainId,optional"`
}

func (a *GetDomainArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.DomainName, "The domain's fully qualified name. Set this or domainId, not both.")
	ann.Describe(&a.DomainId, "The Modal-assigned domain ID. Set this or domainName, not both.")
}

// GetDomainResult is the looked-up domain.
type GetDomainResult struct {
	DomainId          string            `pulumi:"domainId"`
	DomainName        string            `pulumi:"domainName"`
	CertificateStatus string            `pulumi:"certificateStatus"`
	Verified          bool              `pulumi:"verified"`
	DnsRecords        []DomainDNSRecord `pulumi:"dnsRecords"`
}

func (r *GetDomainResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.DomainId, "The Modal-assigned identifier of the domain.")
	ann.Describe(&r.DomainName, "The domain's fully qualified name.")
	ann.Describe(&r.CertificateStatus, "The certificate status: `pending`, `issued`, `failed` or `revoked`.")
	ann.Describe(&r.Verified, "True once the certificate has been issued.")
	ann.Describe(&r.DnsRecords, "The DNS records the domain needs.")
}

func (f *GetDomain) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up a custom domain registered with the Modal workspace by name or ID. Use the Domain resource to register one.")
}

func (f *GetDomain) Invoke(ctx context.Context, req infer.FunctionRequest[GetDomainArgs]) (infer.FunctionResponse[GetDomainResult], error) {
	out := infer.FunctionResponse[GetDomainResult]{}
	id, name := derefStr(req.Input.DomainId), derefStr(req.Input.DomainName)
	if id == "" && name == "" {
		return out, fmt.Errorf("modal: set domainName or domainId")
	}
	if id != "" && name != "" {
		return out, fmt.Errorf("modal: set only one of domainName and domainId")
	}
	client, _, closeFn, err := lookupSession(ctx, nil)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	d, err := listDomain(rpcCtx, client, id, name)
	if err != nil {
		return out, fmt.Errorf("modal: look up domain %q: %w", id+name, err)
	}
	if d == nil {
		return out, fmt.Errorf("modal: domain %q was not found in the workspace", id+name)
	}
	var st DomainState
	applyDomain(&st, d)
	out.Output = GetDomainResult{
		DomainId: st.DomainId, DomainName: st.DomainName, CertificateStatus: st.CertificateStatus,
		Verified: st.Verified, DnsRecords: st.DnsRecords,
	}
	return out, nil
}
