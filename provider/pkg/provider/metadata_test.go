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
	"strings"
	"testing"

	pgp "github.com/pulumi/pulumi-go-provider"
)

// ---------------------------------------------------------------------------
// Registry metadata contract
//
// A mocked-construction test cannot catch a missing or fabricated Go SDK
// import path: it never exercises the schema.Metadata defaulting logic that
// pulumi-go-provider's infer.NewProviderBuilder runs when WithGoImportPath is
// left unset. These tests read the actual generated schema, the same
// artifact the Pulumi Registry and `pulumi package add` consult.
// ---------------------------------------------------------------------------

func mustMetadataSchema(t *testing.T) map[string]any {
	t.Helper()
	prov, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	spec, err := pgp.GetSchema(context.Background(), "modal", "0.1.0", prov)
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}
	return map[string]any{
		"displayName": spec.DisplayName,
		"publisher":   spec.Publisher,
		"license":     spec.License,
		"homepage":    spec.Homepage,
		"repository":  spec.Repository,
		"keywords":    spec.Keywords,
		"goLanguage":  string(spec.Language["go"]),
		"description": spec.Description,
		"pluginURL":   spec.PluginDownloadURL,
		"language": map[string]string{
			"nodejs": string(spec.Language["nodejs"]),
			"python": string(spec.Language["python"]),
			"csharp": string(spec.Language["csharp"]),
			"java":   string(spec.Language["java"]),
		},
	}
}

func TestMetadata_DisplayNameIsHumanBranded(t *testing.T) {
	m := mustMetadataSchema(t)
	if got := m["displayName"].(string); got != "Modal" {
		t.Errorf("DisplayName = %q, want %q", got, "Modal")
	}
}

func TestMetadata_PublisherIsPulumiLabs(t *testing.T) {
	m := mustMetadataSchema(t)
	if got := m["publisher"].(string); got != "Pulumi Labs" {
		t.Errorf("Publisher = %q, want %q", got, "Pulumi Labs")
	}
}

func TestMetadata_LicenseIsSet(t *testing.T) {
	m := mustMetadataSchema(t)
	if got := m["license"].(string); got != "Apache-2.0" {
		t.Errorf("License = %q, want %q", got, "Apache-2.0")
	}
}

func TestMetadata_HomepageAndRepositoryAreSet(t *testing.T) {
	m := mustMetadataSchema(t)
	if got := m["homepage"].(string); got == "" {
		t.Error("Homepage is empty")
	}
	if got := m["repository"].(string); got == "" {
		t.Error("Repository is empty")
	}
}

func TestMetadata_KeywordsStartWithPulumi(t *testing.T) {
	m := mustMetadataSchema(t)
	kws := m["keywords"].([]string)
	if len(kws) == 0 || kws[0] != "pulumi" {
		t.Errorf("Keywords = %v, want first element %q", kws, "pulumi")
	}
}

// TestMetadata_GoImportPathIsNotFabricated is the regression test for the
// defect this package was fixed for: without WithGoImportPath (and without
// WithRepository/WithNamespace set to values that happen to compute the right
// shape), pulumi-go-provider's infer.NewProviderBuilder fabricates
// "local-package/sdk/go/<pkg>", which is not a resolvable Go module path.
func TestMetadata_GoImportPathIsNotFabricated(t *testing.T) {
	m := mustMetadataSchema(t)
	goLang := m["goLanguage"].(string)
	if strings.Contains(goLang, "local-package") {
		t.Fatalf("Go SDK importBasePath was fabricated by the framework default: %s", goLang)
	}
	const want = "github.com/pulumi-labs/pulumi-modal/sdk/go/modal"
	if !strings.Contains(goLang, want) {
		t.Errorf("Go SDK importBasePath = %s, want it to contain %q", goLang, want)
	}
}

func TestMetadata_PluginDownloadURLIsPulumiLabsRepo(t *testing.T) {
	m := mustMetadataSchema(t)
	const want = "github://api.github.com/pulumi-labs/pulumi-modal"
	if got := m["pluginURL"].(string); got != want {
		t.Errorf("PluginDownloadURL = %q, want %q", got, want)
	}
}

func TestMetadata_IsMarkedExperimental(t *testing.T) {
	m := mustMetadataSchema(t)
	if got := m["description"].(string); !strings.Contains(got, "Experimental") {
		t.Errorf("Description = %q, want it to be marked experimental", got)
	}
	langs := m["language"].(map[string]string)
	if got := langs["nodejs"]; !strings.Contains(got, "Experimental") {
		t.Errorf("nodejs packageDescription is not marked experimental: %s", got)
	}
}

// TestMetadata_LanguageMapCarriesPulumiLabsNames guards the language map: the
// package names, plus respectSchemaVersion without which gen-sdk --version is
// ignored and the .NET SDK does not build.
func TestMetadata_LanguageMapCarriesPulumiLabsNames(t *testing.T) {
	m := mustMetadataSchema(t)
	langs := m["language"].(map[string]string)
	goLang := m["goLanguage"].(string)
	want := map[string][]string{
		"nodejs": {`"packageName":"@pulumi-labs/modal"`, `"respectSchemaVersion":true`},
		"python": {`"packageName":"pulumi_labs_modal"`, `"respectSchemaVersion":true`},
		"csharp": {`"rootNamespace":"Pulumi.Labs"`, `"respectSchemaVersion":true`},
		"java":   {`"basePackage":"com.pulumi.labs"`},
	}
	for lang, subs := range want {
		got := strings.ReplaceAll(langs[lang], " ", "")
		for _, sub := range subs {
			if !strings.Contains(got, sub) {
				t.Errorf("%s language settings %s missing %s", lang, langs[lang], sub)
			}
		}
	}
	if g := strings.ReplaceAll(goLang, " ", ""); !strings.Contains(g, `"respectSchemaVersion":true`) {
		t.Errorf("go language settings %s missing respectSchemaVersion", goLang)
	}
}
