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
	"encoding/json"
	"fmt"
	"sync"

	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// secretReads makes Read return the schema's secret properties as secrets.
//
// pulumi-go-provider applies the `provider:"secret"` tags to Create and Update
// outputs, but a Read response only keeps the secretness of the state it was
// given. On an import there is no state, so a secret the provider recovers
// (Machine.secretKey, ApiKey.secret) was stored as plaintext (#1564, #1577). The
// secret properties are read from the provider's own schema, so a new secret
// field is covered without a second list.
type secretReads struct {
	once    sync.Once
	inputs  map[string]map[string]bool // resource token -> secret input properties
	outputs map[string]map[string]bool // resource token -> secret output properties
	err     error
}

func (s *secretReads) load(ctx context.Context, p pgp.Provider) {
	s.once.Do(func() {
		resp, err := p.GetSchema(ctx, pgp.GetSchemaRequest{})
		if err != nil {
			s.err = err
			return
		}
		var spec struct {
			Resources map[string]struct {
				Properties map[string]struct {
					Secret bool `json:"secret"`
				} `json:"properties"`
				InputProperties map[string]struct {
					Secret bool `json:"secret"`
				} `json:"inputProperties"`
			} `json:"resources"`
		}
		if s.err = json.Unmarshal([]byte(resp.Schema), &spec); s.err != nil {
			return
		}
		s.inputs, s.outputs = map[string]map[string]bool{}, map[string]map[string]bool{}
		for tok, r := range spec.Resources {
			s.inputs[tok], s.outputs[tok] = map[string]bool{}, map[string]bool{}
			for name, prop := range r.InputProperties {
				s.inputs[tok][name] = prop.Secret
			}
			for name, prop := range r.Properties {
				s.outputs[tok][name] = prop.Secret
			}
		}
	})
}

func markSecrets(m property.Map, secret map[string]bool) property.Map {
	for name, isSecret := range secret {
		if !isSecret {
			continue
		}
		if v, ok := m.GetOk(name); ok && !v.IsNull() && !v.Secret() {
			m = m.Set(name, v.WithSecret(true))
		}
	}
	return m
}

// wrapReadSecrets returns p with Read marking the schema's secret properties.
func wrapReadSecrets(p pgp.Provider) pgp.Provider {
	read := p.Read
	if read == nil {
		return p
	}
	var s secretReads
	p.Read = func(ctx context.Context, req pgp.ReadRequest) (pgp.ReadResponse, error) {
		resp, err := read(ctx, req)
		if resp.ID == "" {
			return resp, err
		}
		s.load(ctx, p)
		if s.err != nil {
			// Failing loudly: returning the recovered values unmarked would store
			// them as plaintext in the state.
			return resp, fmt.Errorf("marking secret properties: loading provider schema: %w", s.err)
		}
		tok := string(req.Urn.Type())
		resp.Properties = markSecrets(resp.Properties, s.outputs[tok])
		resp.Inputs = markSecrets(resp.Inputs, s.inputs[tok])
		return resp, err
	}
	return p
}
