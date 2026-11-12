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
	"encoding/json"
	"sort"
	"testing"

	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A value wired in from another resource is unknown at preview and infer decodes it to its zero
// value, so Check must not judge it (the bug class of namespace #1832). Every resource: all inputs
// unknown at once must not fail, and each input unknown alone must not fail on that input.
func TestCheck_UnknownInputsAreNotValidated(t *testing.T) {
	prov, err := newProvider()
	require.NoError(t, err)
	s := readSecretsServer(t, prov)
	resp, err := s.GetSchema(pgp.GetSchemaRequest{})
	require.NoError(t, err)
	var spec struct {
		Resources map[string]struct {
			InputProperties map[string]any `json:"inputProperties"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &spec))
	require.NotEmpty(t, spec.Resources)
	var toks []string
	for k := range spec.Resources {
		toks = append(toks, k)
	}
	sort.Strings(toks)
	unknown := property.New(property.Computed)
	for _, tok := range toks {
		t.Run(tok, func(t *testing.T) {
			urn := resource.NewURN("s", "p", "", tokens.Type(tok), "x")
			all := map[string]property.Value{}
			for n := range spec.Resources[tok].InputProperties {
				all[n] = unknown
			}
			r, err := s.Check(pgp.CheckRequest{Urn: urn, Inputs: property.NewMap(all)})
			require.NoError(t, err)
			assert.Empty(t, r.Failures, "all inputs unknown")
			for n := range spec.Resources[tok].InputProperties {
				r, err := s.Check(pgp.CheckRequest{Urn: urn, Inputs: property.NewMap(map[string]property.Value{n: unknown})})
				require.NoError(t, err)
				for _, f := range r.Failures {
					assert.NotEqual(t, n, f.Property, "%s is unknown, so it must not fail", n)
				}
			}
		})
	}
}

func TestDropUnknownFailures_KeepsKnownSiblings(t *testing.T) {
	failures := []pgp.CheckFailure{
		{Property: "a.x", Reason: "1"}, {Property: "a.y", Reason: "2"},
		{Property: "tags[1].key", Reason: "3"}, {Property: "tags[0].key", Reason: "4"},
	}
	in := property.NewMap(map[string]property.Value{
		"a": property.New(property.NewMap(map[string]property.Value{"x": property.New(property.Computed), "y": property.New(0.0)})),
		"tags": property.New(property.NewArray([]property.Value{
			property.New(property.NewMap(map[string]property.Value{"key": property.New("a")})),
			property.New(property.NewMap(map[string]property.Value{"key": property.New(property.Computed)})),
		})),
	})
	var props []string
	for _, f := range dropUnknownFailures(failures, in) {
		props = append(props, f.Property)
	}
	assert.ElementsMatch(t, []string{"a.y", "tags[0].key"}, props)
	assert.Equal(t, "a.b", stripIndexes("a[0].b"))
}
