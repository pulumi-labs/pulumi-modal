package main

import (
	"github.com/pulumi-labs/pulumi-modal/sdk/go/modal"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// An environment is an isolated namespace inside the Modal workspace for Apps, Dicts and Images.
		env, err := modal.NewEnvironment(ctx, "env", &modal.EnvironmentArgs{
			Name:               pulumi.String("platform"),
			MaxConcurrentTasks: pulumi.Int(20),
		})
		if err != nil {
			return err
		}

		// An App groups Functions and deployments; referencing env.Name makes the dependency implicit.
		app, err := modal.NewApp(ctx, "app", &modal.AppArgs{
			Name:        pulumi.String("platform-app"),
			Environment: env.Name,
			Tags: pulumi.StringMap{
				"team":      pulumi.String("ml"),
				"managedBy": pulumi.String("pulumi"),
			},
		})
		if err != nil {
			return err
		}

		// A Dict is a durable key-value store shared between Modal Functions.
		state, err := modal.NewDict(ctx, "state", &modal.DictArgs{
			Name:        pulumi.String("platform-state"),
			Environment: env.Name,
		})
		if err != nil {
			return err
		}

		// An Image is built from a registry base plus Dockerfile commands and published under a stable name.
		image, err := modal.NewImage(ctx, "base", &modal.ImageArgs{
			BaseImage: pulumi.String("python:3.12-slim"),
			DockerfileCommands: pulumi.StringArray{
				pulumi.String("RUN pip install numpy==2.1.3"),
			},
			Name:        pulumi.String("platform-base"),
			Environment: env.Name,
		})
		if err != nil {
			return err
		}

		// Look up the workspace the provider token belongs to, then its default environment.
		workspace := modal.GetWorkspaceOutput(ctx, modal.GetWorkspaceOutputArgs{}, nil)
		defaultEnv := modal.LookupEnvironmentOutput(ctx, modal.LookupEnvironmentOutputArgs{
			Name: workspace.DefaultEnvironment(),
		}, nil)

		ctx.Export("environmentId", env.EnvironmentId)
		ctx.Export("appId", app.AppId)
		ctx.Export("dictId", state.DictId)
		ctx.Export("imageId", image.ImageId)
		ctx.Export("workspaceName", workspace.WorkspaceName())
		ctx.Export("defaultEnvironmentWebSuffix", defaultEnv.WebSuffix())
		return nil
	})
}
