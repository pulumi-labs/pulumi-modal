// ML training pipeline — Modal
//
// Provisions the durable Modal objects a training pipeline needs: a
// Secret carrying the Hugging Face Hub token training Functions use to
// pull gated model weights/datasets, a Volume for checkpoint storage that
// persists across training runs, and a Queue for dispatching training job
// requests to worker Functions.
//
// Your Modal application code looks these up by name at runtime:
//
//	secret = modal.Secret.from_name("training-secrets")
//	volume = modal.Volume.from_name("training-checkpoints")
//	queue = modal.Queue.from_name("training-jobs")
package main

import (
	"github.com/pulumi/pulumi-modal/sdk/go/modal"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		hfToken := cfg.RequireSecret("hfToken")

		checkpointVolumeName := cfg.Get("checkpointVolumeName")
		if checkpointVolumeName == "" {
			checkpointVolumeName = "training-checkpoints"
		}
		trainingQueueName := cfg.Get("trainingQueueName")
		if trainingQueueName == "" {
			trainingQueueName = "training-jobs"
		}

		// Hugging Face Hub token training Functions use to pull gated
		// model weights/datasets.
		secret, err := modal.NewSecret(ctx, "secret", &modal.SecretArgs{
			Name: pulumi.String("training-secrets"),
			Values: pulumi.StringMap{
				"HF_TOKEN": hfToken,
			},
		})
		if err != nil {
			return err
		}

		// Durable checkpoint storage that persists across training runs.
		volume, err := modal.NewVolume(ctx, "volume", &modal.VolumeArgs{
			Name:    pulumi.String(checkpointVolumeName),
			Version: pulumi.String("v2"),
		})
		if err != nil {
			return err
		}

		// Queue a training-job dispatcher enqueues onto; worker Functions
		// pull from it to run training jobs asynchronously.
		queue, err := modal.NewQueue(ctx, "queue", &modal.QueueArgs{
			Name: pulumi.String(trainingQueueName),
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
