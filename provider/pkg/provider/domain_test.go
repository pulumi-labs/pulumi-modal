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
	"testing"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

func domainIn(name string, verify bool) property.Map {
	m := map[string]property.Value{"domainName": str(name)}
	if verify {
		m["verify"] = property.New(true)
	}
	return pm(m)
}

// #1538: a custom domain is registered with its DNS records, verified in place
// once DNS is ready, refreshes without drift, and a destroy only forgets it.
func TestDomain_Lifecycle(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Domain", "d")

	in := domainIn("api.example.com", false)
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.NoError(t, err)
	require.Equal(t, "do-1", created.ID)
	require.Equal(t, "do-1", created.Properties.Get("domainId").AsString())
	require.Equal(t, "pending", created.Properties.Get("certificateStatus").AsString())
	require.False(t, created.Properties.Get("verified").AsBool())
	recs := created.Properties.Get("dnsRecords").AsArray()
	require.Equal(t, 2, recs.Len())
	require.Equal(t, "cname", recs.Get(0).AsMap().Get("type").AsString())
	require.Equal(t, "txt", recs.Get(1).AsMap().Get("type").AsString())
	require.Equal(t, "2030-01-01T00:00:00Z", created.Properties.Get("createdAt").AsString())
	require.Zero(t, f.domains["do-1"].verifies, "verify is off by default")

	// A refresh changes nothing.
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: in})
	require.NoError(t, err)
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: in, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)
	require.Zero(t, f.domains["do-1"].verifies)

	// Turning verify on updates in place and issues the certificate.
	f.domainDNSReady = true
	next := domainIn("api.example.com", true)
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: next, OldInputs: in})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["verify"].Kind)
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: next, OldInputs: in})
	require.NoError(t, err)
	require.Equal(t, "issued", upd.Properties.Get("certificateStatus").AsString())
	require.True(t, upd.Properties.Get("verified").AsBool())
	require.Equal(t, 1, f.domains["do-1"].verifies)

	// Once issued the refresh makes no further verify call and nothing diffs.
	read, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: upd.Properties, Inputs: next})
	require.NoError(t, err)
	require.Equal(t, 1, f.domains["do-1"].verifies)
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: next, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// A name that differs only by case is the same domain: no replace.
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: domainIn("API.example.com", true), OldInputs: next})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Changing the name replaces.
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: domainIn("other.example.com", true), OldInputs: next})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["domainName"].Kind)

	// Destroy forgets: the domain stays registered in Modal.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: upd.Properties}))
	require.Contains(t, f.domains, "do-1")
}

func TestDomain_CreateWithVerifyAndAdopt(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Domain", "d")

	// verify on create, DNS not ready yet: the domain stays pending, no error.
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: domainIn("a.example.com", true)})
	require.NoError(t, err)
	require.Equal(t, "pending", created.Properties.Get("certificateStatus").AsString())
	require.Equal(t, 1, f.domains[created.ID].verifies)

	// A refresh with verify on retries the check and picks up the issued certificate.
	f.domainDNSReady = true
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: domainIn("a.example.com", true)})
	require.NoError(t, err)
	require.Equal(t, "issued", read.Properties.Get("certificateStatus").AsString())

	// Destroy then up again: the registered domain is adopted, not duplicated.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: read.Properties}))
	again, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: domainIn("A.example.com", false)})
	require.NoError(t, err)
	require.Equal(t, created.ID, again.ID)
	require.Len(t, f.domains, 1)
	require.Equal(t, "issued", again.Properties.Get("certificateStatus").AsString())

	// Preview creates nothing.
	f.domains = map[string]*fakeDomain{}
	_, err = s.Create(pgp.CreateRequest{Urn: urn, Properties: domainIn("b.example.com", false), DryRun: true})
	require.NoError(t, err)
	require.Empty(t, f.domains)

	_, err = s.Create(pgp.CreateRequest{Urn: urn, Properties: domainIn(" ", false)})
	require.Error(t, err)
}

func TestDomain_ImportAndDeletedOutside(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Domain", "d")
	f.domains["do-9"] = &fakeDomain{id: "do-9", name: "imp.example.com", status: pb.CertificateStatus_CERTIFICATE_STATUS_ISSUED}

	imp, err := s.Read(pgp.ReadRequest{ID: "do-9", Urn: urn, Properties: property.NewMap(nil), Inputs: property.NewMap(nil)})
	require.NoError(t, err)
	require.Equal(t, "do-9", imp.ID)
	require.Equal(t, "imp.example.com", imp.Inputs.Get("domainName").AsString())
	require.True(t, imp.Properties.Get("verified").AsBool())

	_, err = s.Read(pgp.ReadRequest{ID: "do-nope", Urn: urn, Properties: property.NewMap(nil), Inputs: property.NewMap(nil)})
	require.ErrorContains(t, err, "does not exist")

	// Registration removed outside Pulumi: a refresh drops it from state.
	delete(f.domains, "do-9")
	gone, err := s.Read(pgp.ReadRequest{ID: "do-9", Urn: urn, Properties: imp.Properties, Inputs: imp.Inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)
}

func TestGetDomain(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	f.domains["do-3"] = &fakeDomain{id: "do-3", name: "look.example.com", status: pb.CertificateStatus_CERTIFICATE_STATUS_ISSUED}

	out, err := invoke(t, s, "getDomain", map[string]property.Value{"domainName": str("look.example.com")})
	require.NoError(t, err)
	require.Equal(t, "do-3", getStr(t, out, "domainId"))
	require.Equal(t, "issued", getStr(t, out, "certificateStatus"))
	require.Equal(t, 2, out.Get("dnsRecords").AsArray().Len())

	out, err = invoke(t, s, "getDomain", map[string]property.Value{"domainId": str("do-3")})
	require.NoError(t, err)
	require.Equal(t, "look.example.com", getStr(t, out, "domainName"))

	_, err = invoke(t, s, "getDomain", map[string]property.Value{"domainName": str("nope.example.com")})
	require.ErrorContains(t, err, "was not found")
	_, err = invoke(t, s, "getDomain", map[string]property.Value{"domainId": str("do-3"), "domainName": str("look.example.com")})
	require.ErrorContains(t, err, "only one")
	_, err = invoke(t, s, "getDomain", map[string]property.Value{})
	require.ErrorContains(t, err, "domainName or domainId")
}
