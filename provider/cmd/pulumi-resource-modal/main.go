// Command pulumi-resource-modal is the entry point for the Modal Pulumi
// provider binary. It is invoked by the Pulumi engine and serves the
// provider gRPC protocol over stdin/stdout.
package main

import (
	"context"
	"os"

	pgp "github.com/pulumi/pulumi-go-provider"

	"github.com/pulumi/autoregistry/lib/providers/modal/provider/pkg/provider"
	"github.com/pulumi/autoregistry/lib/providers/modal/provider/pkg/version"
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
