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

func dollars(v float64) property.Value { return property.New(v) }

// #1535: the cycle budget is an in-place Environment input, read back on
// refresh, importable, and left alone while unset.
func TestEnvironment_CycleBudget(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Environment", "e")

	// Created with a budget.
	inputs := pm(map[string]property.Value{"name": str("staging"), "cycleBudgetDollars": dollars(50)})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	require.Equal(t, 50.0, created.Properties.Get("cycleBudgetDollars").AsNumber())
	require.Equal(t, 50.0, *f.envs[created.ID].budget)

	// A refresh reads it and the usage does not leak into state.
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, 50.0, read.Properties.Get("cycleBudgetDollars").AsNumber())
	d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Drift made outside Pulumi shows up as an update.
	v := 80.0
	f.envs[created.ID].budget = &v
	read, err = s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	require.Equal(t, pgp.Update, d.DetailedDiff["cycleBudgetDollars"].Kind)

	// An update sets the new value in place.
	next := pm(map[string]property.Value{"name": str("staging"), "cycleBudgetDollars": dollars(120)})
	upd, err := s.Update(pgp.UpdateRequest{ID: created.ID, Urn: urn, State: read.Properties, Inputs: next, OldInputs: inputs})
	require.NoError(t, err)
	require.Equal(t, 120.0, upd.Properties.Get("cycleBudgetDollars").AsNumber())
	require.Equal(t, 120.0, *f.envs[created.ID].budget)

	// Unset leaves the budget as is.
	unset := pm(map[string]property.Value{"name": str("staging")})
	d, err = s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: upd.Properties, Inputs: unset, OldInputs: next})
	require.NoError(t, err)
	requireNoChanges(t, d)

	// Import from the ID alone adopts the budget; an environment without one stays unset.
	imp, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: property.NewMap(nil), Inputs: property.NewMap(nil)})
	require.NoError(t, err)
	require.Equal(t, 120.0, imp.Inputs.Get("cycleBudgetDollars").AsNumber())
	plain, err := s.Create(pgp.CreateRequest{Urn: modalUrn("Environment", "p"), Properties: pm(map[string]property.Value{"name": str("plain")})})
	require.NoError(t, err)
	require.False(t, hasProp(plain.Properties, "cycleBudgetDollars"))
	imp, err = s.Read(pgp.ReadRequest{ID: plain.ID, Urn: modalUrn("Environment", "p"), Properties: property.NewMap(nil), Inputs: property.NewMap(nil)})
	require.NoError(t, err)
	require.False(t, hasProp(imp.Inputs, "cycleBudgetDollars"))
}

// A budget read that fails must not break refresh: the recorded budget stays.
func TestEnvironment_RefreshSurvivesBudgetReadFailure(t *testing.T) {
	f, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Environment", "e")
	inputs := pm(map[string]property.Value{"name": str("staging"), "cycleBudgetDollars": dollars(50)})
	created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: inputs})
	require.NoError(t, err)
	f.envs[created.ID].budgetErr = true
	read, err := s.Read(pgp.ReadRequest{ID: created.ID, Urn: urn, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	require.Equal(t, created.ID, read.ID)
	require.Equal(t, 50.0, read.Properties.Get("cycleBudgetDollars").AsNumber())
}

func TestEnvironment_NegativeBudgetIsRejected(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	chk, err := s.Check(pgp.CheckRequest{Urn: modalUrn("Environment", "e"), Inputs: pm(map[string]property.Value{"name": str("x"), "cycleBudgetDollars": dollars(-1)})})
	require.NoError(t, err)
	require.NotEmpty(t, chk.Failures)
	chk, err = s.Check(pgp.CheckRequest{Urn: modalUrn("Environment", "e"), Inputs: pm(map[string]property.Value{"name": str("x"), "cycleBudgetDollars": property.New(property.Computed)})})
	require.NoError(t, err)
	require.Empty(t, chk.Failures)
}

func hasProp(m property.Map, key string) bool {
	_, ok := m.GetOk(key)
	return ok
}
