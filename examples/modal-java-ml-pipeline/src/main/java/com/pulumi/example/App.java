package com.pulumi.example;

import com.pulumi.Pulumi;
import com.pulumi.modal.Secret;
import com.pulumi.modal.SecretArgs;
import com.pulumi.modal.Volume;
import com.pulumi.modal.VolumeArgs;
import com.pulumi.modal.Queue;
import com.pulumi.modal.QueueArgs;
import java.util.Map;

// This example provisions the shared infrastructure backing an ML training and
// inference pipeline on Modal: a Secret holding model-hub and API credentials,
// a Volume for durable checkpoint storage, and a Queue for dispatching training
// job requests. The Modal Functions that actually run the training/inference
// code (defined separately in the user's Modal app, typically Python) reference
// these resources at runtime via modal.Secret.from_name("inference-secrets"),
// modal.Volume.from_name("checkpoint-storage"), and
// modal.Queue.from_name("training-jobs") respectively.
public class App {
    public static void main(String[] args) {
        Pulumi.run(ctx -> {
            // Credentials Modal Functions read at runtime via modal.Secret.from_name("inference-secrets").
            var secret = new Secret("inference-secrets",
                SecretArgs.builder()
                    .name("inference-secrets")
                    .values(Map.of(
                        "HF_TOKEN", "hf_placeholder_token",
                        "OPENAI_API_KEY", "sk_placeholder"
                    ))
                    .build());

            // Durable model checkpoint storage mounted via modal.Volume.from_name("checkpoint-storage").
            var checkpoints = new Volume("checkpoint-storage",
                VolumeArgs.builder()
                    .name("checkpoint-storage")
                    .version("v2")
                    .build());

            // Dispatch queue for training job requests, consumed via modal.Queue.from_name("training-jobs").
            var trainingJobs = new Queue("training-jobs",
                QueueArgs.builder()
                    .name("training-jobs")
                    .build());

            ctx.export("secretId", secret.secretId());
            ctx.export("checkpointVolumeId", checkpoints.volumeId());
            ctx.export("trainingQueueId", trainingJobs.queueId());
        });
    }
}
