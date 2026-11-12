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
	"fmt"
	"strings"

	unkpgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// unknownPaths lists the property paths ("a.b", "tags[1].key") that hold a preview-time unknown
// anywhere at or below them. infer decodes an unknown into its zero value, so a validator that
// looked at it would reject a program that wires an output of another resource into an input
// (the same bug as namespace #1832). Check runs its validators on the decoded args, so every
// Check passes its failures through dropUnknownFailures.
func unknownPaths(v property.Value, path string, out map[string]bool) {
	switch {
	case v.IsComputed():
		out[path] = true
	case v.IsMap():
		for k, c := range v.AsMap().AsMap() {
			p := k
			if path != "" {
				p = path + "." + k
			}
			unknownPaths(c, p, out)
		}
	case v.IsArray():
		for i, c := range v.AsArray().AsSlice() {
			unknownPaths(c, fmt.Sprintf("%s[%d]", path, i), out)
		}
	}
}

// pathCovers reports whether a is b or one of its ancestors (on a path boundary).
func pathCovers(a, b string) bool {
	return b == a || strings.HasPrefix(b, a+".") || strings.HasPrefix(b, a+"[")
}

// stripIndexes turns "a[0].b" into "a.b".
func stripIndexes(p string) string {
	var b strings.Builder
	depth := 0
	for _, r := range p {
		switch {
		case r == '[':
			depth++
		case r == ']':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dropUnknownFailures removes the failures that are about a value which is not known yet: a
// failure on an unknown property, below one, or on a container holding one. Known values are
// still validated in full (tags[0].key next to an unknown tags[1].key), and the apply-time
// Check sees every value. A failure named without an index is compared without indexes, which
// can only skip more.
func dropUnknownFailures(failures []unkpgp.CheckFailure, inputs property.Map) []unkpgp.CheckFailure {
	if len(failures) == 0 {
		return failures
	}
	unknown := map[string]bool{}
	unknownPaths(property.New(inputs), "", unknown)
	delete(unknown, "")
	if len(unknown) == 0 {
		return failures
	}
	kept := failures[:0:0]
	for _, f := range failures {
		hit := false
		for u := range unknown {
			prop, uu := f.Property, u
			if !strings.Contains(prop, "[") {
				uu = stripIndexes(u)
			}
			if pathCovers(uu, prop) || pathCovers(prop, uu) {
				hit = true
				break
			}
		}
		if !hit {
			kept = append(kept, f)
		}
	}
	return kept
}
