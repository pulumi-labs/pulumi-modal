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
	"os"
	"path/filepath"
	"regexp"
	"testing"

	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

// goModSDKVersion reads the vendored Modal SDK version from the provider's
// go.mod, searching upward from the test's directory.
func goModSDKVersion(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(modalSDKModule) + `\s+v(\S+)`).FindSubmatch(data)
			require.NotNil(t, m, "go.mod does not require %s", modalSDKModule)
			return string(m[1])
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found")
		dir = parent
	}
}

// The control plane validates x-modal-libmodal-version against the SDK
// versions it knows, so the header must be "modal-go/<vendored SDK version>".
// The fallback used without build info must not drift from go.mod.
func TestLibmodalVersionTracksVendoredSDK(t *testing.T) {
	want := goModSDKVersion(t)
	require.Equal(t, want, fallbackModalSDKVersion, "update fallbackModalSDKVersion together with the Modal SDK in go.mod")
	require.Equal(t, want, modalSDKVersion())
	require.Equal(t, "modal-go/"+want, libmodalVersionHeader())
}

// The fake rejects an unknown libmodal version on AppGetOrCreate like the
// live API; a successful App create proves the headers are accepted.
func TestApp_CreateSendsKnownClientHeaders(t *testing.T) {
	_, addr := startFakeModal(t)
	s := serverFor(t, addr, "")
	_, err := s.Create(pgp.CreateRequest{Urn: modalUrn("App", "a"), Properties: pm(map[string]property.Value{"name": str("hdr-app")})})
	require.NoError(t, err)
}
