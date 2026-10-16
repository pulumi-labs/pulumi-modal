package main

import (
	"github.com/pulumi/pulumi-modal/sdk/go/modal"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		secretName := cfg.Get("secretName")
		if secretName == "" {
			secretName = "app-secrets"
		}
		volumeName := cfg.Get("volumeName")
		if volumeName == "" {
			volumeName = "app-volume"
		}
		queueName := cfg.Get("queueName")
		if queueName == "" {
			queueName = "app-queue"
		}

		secret, err := modal.NewSecret(ctx, "secret", &modal.SecretArgs{
			Name: pulumi.String(secretName),
			Values: pulumi.StringMap{
				"API_KEY": pulumi.String("changeme"),
			},
		})
		if err != nil {
			return err
		}

		volume, err := modal.NewVolume(ctx, "volume", &modal.VolumeArgs{
			Name: pulumi.String(volumeName),
		})
		if err != nil {
			return err
		}

		queue, err := modal.NewQueue(ctx, "queue", &modal.QueueArgs{
			Name: pulumi.String(queueName),
		})
		if err != nil {
			return err
		}

		ctx.Export("secretId", secret.SecretId)
		ctx.Export("volumeId", volume.VolumeId)
		ctx.Export("queueId", queue.QueueId)
		return nil
	})
}
