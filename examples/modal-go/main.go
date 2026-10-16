package main

import (
	"github.com/pulumi/pulumi-modal/sdk/go/modal"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Secrets mount as environment variables into Modal Functions at runtime.
		secret, err := modal.NewSecret(ctx, "api-keys", &modal.SecretArgs{
			Name: pulumi.String("api-keys"),
			Values: pulumi.StringMap{
				"STRIPE_API_KEY":   pulumi.String("sk_live_xxx_placeholder"),
				"SENDGRID_API_KEY": pulumi.String("SG.xxx_placeholder"),
			},
		})
		if err != nil {
			return err
		}

		// Volumes are durable network filesystems that Modal Functions mount for shared, persistent state (e.g. model weights).
		volume, err := modal.NewVolume(ctx, "model-cache", &modal.VolumeArgs{
			Name:    pulumi.String("model-cache"),
			Version: pulumi.String("v2"),
		})
		if err != nil {
			return err
		}

		// Queues are durable FIFO/LIFO dispatch queues used to coordinate work between Modal Functions.
		queue, err := modal.NewQueue(ctx, "job-queue", &modal.QueueArgs{
			Name: pulumi.String("job-queue"),
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
