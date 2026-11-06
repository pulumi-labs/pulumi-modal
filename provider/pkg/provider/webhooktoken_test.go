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

	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

func TestWebhookToken_Lifecycle(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("WebhookToken", "proxy-auth")

	inputs := pm(map[string]property.Value{"name": str("edge"), "scoped": property.New(true)})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	state := created.Properties
	require.Contains(t, created.ID, "wk-")
	require.Equal(t, created.ID, getStr(t, state, "webhookTokenId"))
	require.Equal(t, "ws-secret-"+created.ID, getStr(t, state, "webhookTokenSecret"))
	require.NotEmpty(t, getStr(t, state, "createdAt"))

	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: inputs, OldInputs: inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// The name updates in place; scoped replaces.
	renamed := pm(map[string]property.Value{"name": str("edge-2"), "scoped": property.New(true)})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: renamed, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["name"].Kind)
	require.False(t, d.DeleteBeforeReplace)
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: pm(map[string]property.Value{"name": str("edge"), "scoped": property.New(false)}), OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["scoped"].Kind)

	up, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: state, Inputs: renamed, OldInputs: inputs})
	require.NoError(t, err)
	fake.mu.Lock()
	require.Equal(t, "edge-2", fake.webhookTokens[created.ID].name)
	fake.mu.Unlock()
	require.Equal(t, "ws-secret-"+created.ID, getStr(t, up.Properties, "webhookTokenSecret"), "the secret survives an update")
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: up.Properties, Inputs: renamed, OldInputs: renamed})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// A rename in the Modal UI is drift; a refresh keeps the secret.
	fake.mu.Lock()
	fake.webhookTokens[created.ID].name = "ui-name"
	fake.mu.Unlock()
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: up.Properties, Inputs: renamed})
	require.NoError(t, err)
	require.Equal(t, "ui-name", getStr(t, read.Inputs, "name"))
	require.Equal(t, "ws-secret-"+created.ID, getStr(t, read.Properties, "webhookTokenSecret"))
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: renamed, OldInputs: read.Inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["name"].Kind)

	// Import by ID alone: no secret is recoverable, the program that matches plans nothing.
	imp, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn})
	require.NoError(t, err)
	require.Equal(t, "ui-name", getStr(t, imp.Inputs, "name"))
	_, hasSecret := imp.Properties.GetOk("webhookTokenSecret")
	require.False(t, hasSecret)
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: imp.Properties, Inputs: imp.Inputs, OldInputs: imp.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Deleted outside Pulumi.
	fake.mu.Lock()
	delete(fake.webhookTokens, created.ID)
	fake.mu.Unlock()
	gone, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: state, Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, gone.ID)

	// Delete is idempotent once the token is gone, and removes a live one.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: created.ID, Urn: urn, Properties: state}))
	again, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: pm(map[string]property.Value{"name": str("plain")})})
	require.NoError(t, err)
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: again.ID, Urn: urn, Properties: again.Properties}))
	fake.mu.Lock()
	require.Empty(t, fake.webhookTokens)
	fake.mu.Unlock()
}

func TestWebhookToken_CreateErrors(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	_, err := s.Create(pgp.CreateRequest{Urn: modalUrn("WebhookToken", "a"), Properties: pm(map[string]property.Value{"name": str("  ")})})
	require.ErrorContains(t, err, "name must not be empty")
}

func TestGetWebhookToken(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	fake.mu.Lock()
	fake.webhookTokens["wk-1"] = &fakeWebhookToken{id: "wk-1", name: "edge", scoped: true, created: 1893456000}
	fake.webhookTokens["wk-2"] = &fakeWebhookToken{id: "wk-2", name: "dup"}
	fake.webhookTokens["wk-3"] = &fakeWebhookToken{id: "wk-3", name: "dup"}
	fake.mu.Unlock()
	out, err := invoke(t, s, "getWebhookToken", map[string]property.Value{"name": str("edge")})
	require.NoError(t, err)
	require.Equal(t, "wk-1", getStr(t, out, "webhookTokenId"))
	require.True(t, out.Get("scoped").AsBool())
	require.NotEmpty(t, getStr(t, out, "createdAt"))
	_, hasSecret := out.GetOk("webhookTokenSecret")
	require.False(t, hasSecret, "the lookup never returns a secret")
	_, err = invoke(t, s, "getWebhookToken", map[string]property.Value{"name": str("dup")})
	require.ErrorContains(t, err, "2 webhook tokens are named")
	_, err = invoke(t, s, "getWebhookToken", map[string]property.Value{"name": str("nope")})
	require.ErrorContains(t, err, "was not found")
}
