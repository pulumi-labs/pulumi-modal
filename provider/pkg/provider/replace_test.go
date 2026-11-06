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
// exception only in the other direction: Modal image IDs are content-addressed, so it
// deletes first too (TestImage_ReplaceDeletesFirst, #1613).

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

// Image replaces on any build input change. Modal image IDs are content
// addressed: while the old image exists, building the same definition again
// returns the same ID, and the engine's delete-replaced step would then delete
// the image the replacement just created (#1613). So every replace deletes
// first, which also guarantees a fresh ID.
func TestImage_ReplaceDeletesFirst(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")

	oldIn := pm(map[string]property.Value{"baseImage": str("python:3.12-slim"), "name": str("torch-base")})
	oldImg, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: oldIn})
	require.NoError(t, err)

	for label, newIn := range map[string]property.Map{
		"baseImage": pm(map[string]property.Value{"baseImage": str("python:3.13-slim"), "name": str("torch-base")}),
		"environment": pm(map[string]property.Value{"baseImage": str("python:3.12-slim"), "name": str("torch-base"),
			"environment": str("other")}),
	} {
		d, err := s.Diff(pgp.DiffRequest{ID: oldImg.ID, Urn: urn, State: oldImg.Properties, Inputs: newIn, OldInputs: oldIn})
		require.NoError(t, err, label)
		require.Equal(t, pgp.UpdateReplace, d.DetailedDiff[label].Kind, label)
		require.True(t, d.DeleteBeforeReplace, label)
	}

	// A rename alone is an in-place update, not a replace.
	renamed := pm(map[string]property.Value{"baseImage": str("python:3.12-slim"), "name": str("other-name")})
	d, err := s.Diff(pgp.DiffRequest{ID: oldImg.ID, Urn: urn, State: oldImg.Properties, Inputs: renamed, OldInputs: oldIn})
	require.NoError(t, err)
	require.False(t, d.DeleteBeforeReplace)
}

// The same definition rebuilt after the replaced image was deleted gets a new
// ID, so the replacement survives the engine's replace sequence.
func TestImage_ReplaceOfAnIdenticalDefinitionLeavesTheNewImageAlive(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")

	in := pm(map[string]property.Value{"baseImage": str("python:3.12-slim"), "name": str("torch-base")})
	oldImg, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.NoError(t, err)

	// Without delete-first the same ID comes back (the fake mirrors Modal here).
	same, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.NoError(t, err)
	require.Equal(t, oldImg.ID, same.ID)

	// Delete-before-replace: delete, then create.
	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: oldImg.ID, Urn: urn, Properties: oldImg.Properties}))
	newImg, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.NoError(t, err)
	require.NotEqual(t, oldImg.ID, newImg.ID)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Contains(t, fake.images, newImg.ID)
	require.Equal(t, newImg.ID, fake.tags["main/torch-base:latest"])
}

// Deleting a named image in a non-default environment removes that
// environment's name only, never the same name in the default environment.
func TestImage_DeleteNameInNonDefaultEnvironmentLeavesTheDefaultEnvironmentName(t *testing.T) {
	fake, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	urn := modalUrn("Image", "img")

	in := pm(map[string]property.Value{"baseImage": str("python:3.12-slim"), "name": str("shared"), "environment": str("other")})
	img, err := s.Create(pgp.CreateRequest{Urn: urn, Properties: in})
	require.NoError(t, err)
	fake.mu.Lock()
	fake.images["im-main"] = &fakeImage{id: "im-main", env: "main"}
	fake.tags["main/shared:latest"] = "im-main"
	require.Equal(t, img.ID, fake.tags["other/shared:latest"])
	fake.mu.Unlock()

	require.NoError(t, s.Delete(pgp.DeleteRequest{ID: img.ID, Urn: urn, Properties: img.Properties}))
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.NotContains(t, fake.tags, "other/shared:latest")
	require.Equal(t, "im-main", fake.tags["main/shared:latest"], "the default environment's name must survive")
}
