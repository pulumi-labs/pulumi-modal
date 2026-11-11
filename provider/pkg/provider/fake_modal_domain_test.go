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
	"sort"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// This file extends the fake Modal control plane with the custom Domain RPCs.
// Like the live API there is no delete.

type fakeDomain struct {
	id       string
	name     string
	status   pb.CertificateStatus
	verifies int
}

func (d *fakeDomain) proto() *pb.Domain {
	return pb.Domain_builder{
		DomainId: d.id, DomainName: d.name, CreatedAt: 1893456000, CertificateStatus: d.status,
		DnsRecords: []*pb.DNSRecord{
			pb.DNSRecord_builder{Type: pb.DNSRecordType_DNS_RECORD_TYPE_CNAME, Name: d.name, Value: "domains.modal.example"}.Build(),
			pb.DNSRecord_builder{Type: pb.DNSRecordType_DNS_RECORD_TYPE_TXT, Name: "_modal." + d.name, Value: "verify-" + d.id}.Build(),
		},
	}.Build()
}

func (f *fakeModal) DomainCreate(_ context.Context, req *pb.DomainCreateRequest) (*pb.DomainCreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.domains {
		if d.name == req.GetDomainName() {
			return nil, status.Error(codes.AlreadyExists, "domain already exists")
		}
	}
	d := &fakeDomain{id: f.nextID("do"), name: req.GetDomainName(), status: pb.CertificateStatus_CERTIFICATE_STATUS_PENDING}
	f.domains[d.id] = d
	return pb.DomainCreateResponse_builder{DomainId: d.id, DnsRecords: d.proto().GetDnsRecords()}.Build(), nil
}

func (f *fakeModal) DomainList(context.Context, *pb.DomainListRequest) (*pb.DomainListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.domains))
	for id := range f.domains {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []*pb.Domain
	for _, id := range ids {
		out = append(out, f.domains[id].proto())
	}
	return pb.DomainListResponse_builder{Domains: out}.Build(), nil
}

// DomainCertificateVerify issues the certificate when the test has set dnsReady.
func (f *fakeModal) DomainCertificateVerify(_ context.Context, req *pb.DomainCertificateVerifyRequest) (*pb.DomainCertificateVerifyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.domains[req.GetDomainId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "domain not found")
	}
	d.verifies++
	if f.domainDNSReady {
		d.status = pb.CertificateStatus_CERTIFICATE_STATUS_ISSUED
	}
	return pb.DomainCertificateVerifyResponse_builder{Domain: d.proto()}.Build(), nil
}
