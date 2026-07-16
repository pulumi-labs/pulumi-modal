package main

import (
	"github.com/pulumi/pulumi-modal/sdk/go/modal"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// This example provisions the shared infrastructure backing an ML training and
// inference pipeline on Modal: a Secret holding model-hub and API credentials,
// a Volume for durable checkpoint storage, and a Queue for dispatching training
// job requests. The Modal Functions that actually run the training/inference
// code (defined separately in the user's Modal app, typically Python) reference
// these resources at runtime via modal.Secret.from_name("inference-secrets"),
// modal.Volume.from_name("checkpoint-storage"), and
// modal.Queue.from_name("training-jobs") respectively.
func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		tokenId := cfg.RequireSecret("tokenId")
		tokenSecret := cfg.RequireSecret("tokenSecret")

		// Credentials Modal Functions read at runtime via modal.Secret.from_name("inference-secrets").
		secret, err := modal.NewSecret(ctx, "inference-secrets", &modal.SecretArgs{
			TokenId:     tokenId,
			TokenSecret: tokenSecret,
			Name:        pulumi.String("inference-secrets"),
			Values: pulumi.StringMap{
				"HF_TOKEN":       pulumi.String("hf_placeholder_token"),
				"OPENAI_API_KEY": pulumi.String("sk_placeholder"),
			},
		})
		if err != nil {
			return err
		}

		// Durable model checkpoint storage mounted via modal.Volume.from_name("checkpoint-storage").
		checkpoints, err := modal.NewVolume(ctx, "checkpoint-storage", &modal.VolumeArgs{
			TokenId:     tokenId,
			TokenSecret: tokenSecret,
			Name:        pulumi.String("checkpoint-storage"),
			Version:     pulumi.String("v2"),
		})
		if err != nil {
			return err
		}

		// Dispatch queue for training job requests, consumed via modal.Queue.from_name("training-jobs").
		trainingJobs, err := modal.NewQueue(ctx, "training-jobs", &modal.QueueArgs{
			TokenId:     tokenId,
			TokenSecret: tokenSecret,
			Name:        pulumi.String("training-jobs"),
		})
		if err != nil {
			return err
		}

		ctx.Export("secretId", secret.SecretId)
		ctx.Export("checkpointVolumeId", checkpoints.VolumeId)
		ctx.Export("trainingQueueId", trainingJobs.QueueId)
		return nil
	})
}
