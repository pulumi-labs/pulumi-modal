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
//   secret = modal.Secret.from_name("training-secrets")
//   volume = modal.Volume.from_name("training-checkpoints")
//   queue = modal.Queue.from_name("training-jobs")
package com.pulumi.example;

import com.pulumi.Pulumi;
import com.pulumi.labs.modal.Secret;
import com.pulumi.labs.modal.SecretArgs;
import com.pulumi.labs.modal.Volume;
import com.pulumi.labs.modal.VolumeArgs;
import com.pulumi.labs.modal.Queue;
import com.pulumi.labs.modal.QueueArgs;
import java.util.Map;

public class App {
    public static void main(String[] args) {
        Pulumi.run(ctx -> {
            var config = ctx.config();
            var hfToken = config.requireSecret("hfToken");
            var checkpointVolumeName = config.get("checkpointVolumeName").orElse("training-checkpoints");
            var trainingQueueName = config.get("trainingQueueName").orElse("training-jobs");

            // Hugging Face Hub token training Functions use to pull
            // gated model weights/datasets.
            var secret = new Secret("secret",
                SecretArgs.builder()
                    .name("training-secrets")
                    .values(hfToken.applyValue(t -> Map.of("HF_TOKEN", t)))
                    .build());

            // Durable checkpoint storage that persists across training
            // runs.
            var volume = new Volume("volume",
                VolumeArgs.builder()
                    .name(checkpointVolumeName)
                    .version("v2")
                    .build());

            // Queue a training-job dispatcher enqueues onto; worker
            // Functions pull from it to run training jobs asynchronously.
            var queue = new Queue("queue",
                QueueArgs.builder()
                    .name(trainingQueueName)
                    .build());

            ctx.export("secretId", secret.secretId());
            ctx.export("volumeId", volume.volumeId());
            ctx.export("queueId", queue.queueId());
        });
    }
}
