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

// Command pulumi-resource-modal is the entry point for the Modal Pulumi
// provider binary. It is invoked by the Pulumi engine and serves the
// provider gRPC protocol over stdin/stdout.
package main

import (
	"context"
	"os"

	pgp "github.com/pulumi/pulumi-go-provider"

	"github.com/pulumi-labs/pulumi-modal/provider/pkg/provider"
	"github.com/pulumi-labs/pulumi-modal/provider/pkg/version"
)

func main() {
	p, err := provider.New()
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to construct provider: " + err.Error() + "\n")
		os.Exit(1)
	}
	if err := pgp.RunProvider(context.Background(), "modal", version.Version, p); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
