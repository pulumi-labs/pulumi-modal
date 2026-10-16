// Background job queue — Modal
//
// Provisions a Modal Queue that a web app or scheduler enqueues jobs
// onto, plus a Secret carrying the API key background worker Functions
// use to authenticate callbacks back to your application.
//
// Your Modal application code looks these up by name at runtime:
//
//   queue = modal.Queue.from_name("background-jobs")
//   secret = modal.Secret.from_name("worker-callback-secret")
package com.pulumi.example;

import com.pulumi.Pulumi;
import com.pulumi.modal.Secret;
import com.pulumi.modal.SecretArgs;
import com.pulumi.modal.Queue;
import com.pulumi.modal.QueueArgs;
import java.util.Map;

public class App {
    public static void main(String[] args) {
        Pulumi.run(ctx -> {
            var config = ctx.config();
            var queueName = config.get("queueName").orElse("background-jobs");
            var workerApiKey = config.requireSecret("workerApiKey");

            // API key background workers use to authenticate callbacks
            // to your application once a job completes.
            var secret = new Secret("secret",
                SecretArgs.builder()
                    .name("worker-callback-secret")
                    .values(Map.of("WORKER_API_KEY", workerApiKey))
                    .build());

            // Durable queue a dispatcher enqueues jobs onto; a pool of
            // worker Functions pulls from it to process them
            // asynchronously.
            var queue = new Queue("queue",
                QueueArgs.builder()
                    .name(queueName)
                    .build());

            ctx.export("secretId", secret.secretId());
            ctx.export("queueId", queue.queueId());
        });
    }
}
