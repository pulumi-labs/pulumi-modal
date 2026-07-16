package com.pulumi.example;

import com.pulumi.Pulumi;
import com.pulumi.modal.Secret;
import com.pulumi.modal.SecretArgs;
import com.pulumi.modal.Volume;
import com.pulumi.modal.VolumeArgs;
import com.pulumi.modal.Queue;
import com.pulumi.modal.QueueArgs;
import java.util.Map;

public class App {
    public static void main(String[] args) {
        Pulumi.run(ctx -> {
            var config = ctx.config();
            var tokenId = config.requireSecret("tokenId");
            var tokenSecret = config.requireSecret("tokenSecret");

            // Secrets mount as environment variables into Modal Functions at runtime.
            var secret = new Secret("api-keys",
                SecretArgs.builder()
                    .tokenId(tokenId)
                    .tokenSecret(tokenSecret)
                    .name("api-keys")
                    .values(Map.of(
                        "STRIPE_API_KEY", "sk_live_xxx_placeholder",
                        "SENDGRID_API_KEY", "SG.xxx_placeholder"
                    ))
                    .build());

            // Volumes are durable network filesystems that Modal Functions mount for shared, persistent state (e.g. model weights).
            var volume = new Volume("model-cache",
                VolumeArgs.builder()
                    .tokenId(tokenId)
                    .tokenSecret(tokenSecret)
                    .name("model-cache")
                    .version("v2")
                    .build());

            // Queues are durable FIFO/LIFO dispatch queues used to coordinate work between Modal Functions.
            var queue = new Queue("job-queue",
                QueueArgs.builder()
                    .tokenId(tokenId)
                    .tokenSecret(tokenSecret)
                    .name("job-queue")
                    .build());

            ctx.export("secretId", secret.secretId());
            ctx.export("volumeId", volume.volumeId());
            ctx.export("queueId", queue.queueId());
        });
    }
}
