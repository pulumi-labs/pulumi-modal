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
            // Secrets mount as environment variables into Modal Functions at runtime.
            var secret = new Secret("api-keys",
                SecretArgs.builder()
                    .name("api-keys")
                    .values(Map.of(
                        "STRIPE_API_KEY", "sk_live_xxx_placeholder",
                        "SENDGRID_API_KEY", "SG.xxx_placeholder"
                    ))
                    .build());

            // Volumes are durable network filesystems that Modal Functions mount for shared, persistent state (e.g. model weights).
            var volume = new Volume("model-cache",
                VolumeArgs.builder()
                    .name("model-cache")
                    .version("v2")
                    .build());

            // Queues are durable FIFO/LIFO dispatch queues used to coordinate work between Modal Functions.
            var queue = new Queue("job-queue",
                QueueArgs.builder()
                    .name("job-queue")
                    .build());

            ctx.export("secretId", secret.secretId());
            ctx.export("volumeId", volume.volumeId());
            ctx.export("queueId", queue.queueId());
        });
    }
}
