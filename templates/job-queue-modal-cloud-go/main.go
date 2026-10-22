// Background job queue — Modal
//
// Provisions a Modal Queue that a web app or scheduler enqueues jobs
// onto, plus a Secret carrying the API key background worker Functions
// use to authenticate callbacks back to your application.
//
// Your Modal application code looks these up by name at runtime:
//
//	queue = modal.Queue.from_name("background-jobs")
//	secret = modal.Secret.from_name("worker-callback-secret")
package main

import (
	"github.com/pulumi-labs/pulumi-modal/sdk/go/modal"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		queueName := cfg.Get("queueName")
		if queueName == "" {
			queueName = "background-jobs"
		}
		workerApiKey := cfg.RequireSecret("workerApiKey")

		// API key background workers use to authenticate callbacks to
		// your application once a job completes.
		secret, err := modal.NewSecret(ctx, "secret", &modal.SecretArgs{
			Name: pulumi.String("worker-callback-secret"),
			Values: pulumi.StringMap{
				"WORKER_API_KEY": workerApiKey,
			},
		})
		if err != nil {
			return err
		}

		// Durable queue a dispatcher enqueues jobs onto; a pool of
		// worker Functions pulls from it to process them asynchronously.
		queue, err := modal.NewQueue(ctx, "queue", &modal.QueueArgs{
			Name: pulumi.String(queueName),
		})
		if err != nil {
			return err
		}

		ctx.Export("secretId", secret.SecretId)
		ctx.Export("queueId", queue.QueueId)
		return nil
	})
}
