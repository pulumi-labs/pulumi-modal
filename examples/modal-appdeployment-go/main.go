package main

import (
	"github.com/pulumi-labs/pulumi-modal/sdk/go/modal"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Needs the Modal CLI on this machine (`pip install modal`): `modal deploy` does
// the packaging. Credentials: set modal:tokenId / modal:tokenSecret, or the
// MODAL_TOKEN_ID / MODAL_TOKEN_SECRET environment variables.
func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Runs `modal deploy` for app.py and manages the deployed App. Editing app.py
		// (or anything beside it) redeploys in place on the next `pulumi up`.
		api, err := modal.NewAppDeployment(ctx, "api", &modal.AppDeploymentArgs{
			Name:    pulumi.String("deployment-example"),
			AppFile: pulumi.String("app.py"),
		})
		if err != nil {
			return err
		}

		// A non-human identity for CI/CD. Modal returns its token secret only once;
		// Pulumi keeps it as a secret output.
		ci, err := modal.NewServiceUser(ctx, "ci", &modal.ServiceUserArgs{
			Name:   pulumi.String("deployment-example-ci"),
			Scoped: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

		ctx.Export("appId", api.AppId)
		ctx.Export("functions", api.Functions)
		ctx.Export("webUrls", api.WebUrls)
		ctx.Export("ciTokenId", ci.ServiceTokenId)
		ctx.Export("ciTokenSecret", ci.ServiceTokenSecret)
		return nil
	})
}
