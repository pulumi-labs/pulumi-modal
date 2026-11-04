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

// Modal names are unique per environment, so a replacement that keeps the
// same name in the same environment cannot be created while the original
// still exists (AlreadyExists, pulumi-labs/pulumi-modal#5). These tests pin
// the invariant for every resource that has a unique name: whenever a Diff
// asks for a replace and neither the name nor the effective environment
// changes, DeleteBeforeReplace must be set. Image is the deliberate
// exception, see TestImage_ReplaceKeepingTheNameIsCreateBeforeDelete.

func isReplaceKind(k pgp.DiffKind) bool {
	return k == pgp.AddReplace || k == pgp.UpdateReplace || k == pgp.DeleteReplace
}

func TestReplaceWithSameNameAndEnvironmentDeletesFirst(t *testing.T) {
	type mutation struct {
		label  string
		inputs map[string]property.Value
	}
	cases := []struct {
		typ       string
		base      map[string]property.Value
		mutations []mutation
		// replaceOnly lists the mutation labels that must plan a replace, so a
		// refactor cannot make the invariant pass by never replacing at all.
		replaceOnly []string
	}{
		{
			typ:  "Volume",
			base: map[string]property.Value{"name": str("data"), "fsVersion": str("v2")},
			mutations: []mutation{
				{"fsVersion", map[string]property.Value{"name": str("data"), "fsVersion": str("v1")}},
				{"fsVersion and explicit environment", map[string]property.Value{"name": str("data"), "fsVersion": str("v1"), "environment": str("main")}},
				{"rename", map[string]property.Value{"name": str("data2"), "fsVersion": str("v2")}},
				{"rename and fsVersion", map[string]property.Value{"name": str("data2"), "fsVersion": str("v1")}},
				{"environment", map[string]property.Value{"name": str("data"), "fsVersion": str("v2"), "environment": str("staging")}},
				{"nothing", map[string]property.Value{"name": str("data"), "fsVersion": str("v2")}},
			},
			replaceOnly: []string{"fsVersion", "fsVersion and explicit environment", "rename and fsVersion", "environment"},
		},
		{
			typ:  "Queue",
			base: map[string]property.Value{"name": str("jobs")},
			mutations: []mutation{
				{"rename", map[string]property.Value{"name": str("jobs2")}},
				{"environment", map[string]property.Value{"name": str("jobs"), "environment": str("staging")}},
				{"explicit default environment", map[string]property.Value{"name": str("jobs"), "environment": str("main")}},
			},
			replaceOnly: []string{"rename", "environment"},
		},
		{
			typ:  "Secret",
			base: map[string]property.Value{"name": str("creds"), "values": strs(map[string]string{"K": "v"})},
			mutations: []mutation{
				{"rename", map[string]property.Value{"name": str("creds2"), "values": strs(map[string]string{"K": "v"})}},
				{"environment", map[string]property.Value{"name": str("creds"), "environment": str("staging"), "values": strs(map[string]string{"K": "v"})}},
				{"value", map[string]property.Value{"name": str("creds"), "values": strs(map[string]string{"K": "w"})}},
			},
			replaceOnly: []string{"rename", "environment"},
		},
		{
			typ:  "Dict",
			base: map[string]property.Value{"name": str("cache")},
			mutations: []mutation{
				{"rename", map[string]property.Value{"name": str("cache2")}},
				{"environment", map[string]property.Value{"name": str("cache"), "environment": str("staging")}},
			},
			replaceOnly: []string{"rename", "environment"},
		},
		{
			typ:  "App",
			base: map[string]property.Value{"name": str("trainer")},
			mutations: []mutation{
				{"rename", map[string]property.Value{"name": str("trainer2")}},
				{"environment", map[string]property.Value{"name": str("trainer"), "environment": str("staging")}},
				{"tags", map[string]property.Value{"name": str("trainer"), "tags": strMap(map[string]string{"team": "ml"})}},
			},
			replaceOnly: []string{"rename", "environment"},
		},
	}

	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			_, addr := startFakeModal(t)
			s := serverFor(t, addr, "")
			urn := modalUrn(c.typ, "res")
			base := pm(c.base)
			created, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: base})
			require.NoError(t, err)
			state := created.Properties

			for _, m := range c.mutations {
				in := pm(m.inputs)
				d, err := s.Diff(pgp.DiffRequest{ID: created.ID, Urn: urn, State: state, Inputs: in, OldInputs: base})
				require.NoError(t, err, m.label)

				replaces := false
				for _, pd := range d.DetailedDiff {
					replaces = replaces || isReplaceKind(pd.Kind)
				}
				want := false
				for _, l := range c.replaceOnly {
					want = want || l == m.label
				}
				require.Equal(t, want, replaces, "%s %q: replace planned = %v, want %v (%+v)", c.typ, m.label, replaces, want, d.DetailedDiff)
				if !replaces {
					continue
				}

				_, nameChanged := d.DetailedDiff["name"]
				_, envChanged := d.DetailedDiff["environment"]
				if !nameChanged && !envChanged {
					require.True(t, d.DeleteBeforeReplace,
						"%s %q replaces with the same name and environment, so it must delete first (diff %+v)", c.typ, m.label, d.DetailedDiff)
				}
			}
		})
	}
}

// Image replaces on any build input change and keeps its published name. That
// is safe with the engine's default create-before-delete order: publishing a
// name only repoints the tag at the new image, and deleting the old image
// removes the name only while it still points at the old image. So no
// DeleteBeforeReplace (which would take the name down during the rebuild).
func TestImage_ReplaceKeepingTheNameIsCreateBeforeDelete(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")

	oldIn := pm(map[string]property.Value{"baseImage": str("python:3.12-slim"), "name": str("torch-base")})
	oldImg, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: oldIn})
	require.NoError(t, err)

	newIn := pm(map[string]property.Value{"baseImage": str("python:3.13-slim"), "name": str("torch-base")})
	d, err := s.Diff(pgp.DiffRequest{ID: oldImg.ID, Urn: urn, State: oldImg.Properties, Inputs: newIn, OldInputs: oldIn})
	require.NoError(t, err)
	require.Equal(t, pgp.UpdateReplace, d.DetailedDiff["baseImage"].Kind)
	require.False(t, d.DeleteBeforeReplace)

	// The engine creates the replacement first: the same name is published
	// again without an AlreadyExists error, then the old image is deleted.
	newImg, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: newIn})
	require.NoError(t, err)
	require.NotEqual(t, oldImg.ID, newImg.ID)
	fake.mu.Lock()
	require.Equal(t, newImg.ID, fake.tags["main/torch-base:latest"])
	fake.mu.Unlock()

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: oldImg.ID, Urn: urn, Properties: oldImg.Properties}))
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Equal(t, newImg.ID, fake.tags["main/torch-base:latest"], "deleting the old image must not take the name over")
	require.Contains(t, fake.images, newImg.ID)
	require.NotContains(t, fake.images, oldImg.ID)
}
