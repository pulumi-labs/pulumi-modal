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
            var secretName = config.require("secretName");
            var volumeName = config.require("volumeName");
            var queueName = config.require("queueName");

            var secret = new Secret("secret",
                SecretArgs.builder()
                    .tokenId(tokenId)
                    .tokenSecret(tokenSecret)
                    .name(secretName)
                    .values(Map.of("API_KEY", "changeme"))
                    .build());

            var volume = new Volume("volume",
                VolumeArgs.builder()
                    .tokenId(tokenId)
                    .tokenSecret(tokenSecret)
                    .name(volumeName)
                    .build());

            var queue = new Queue("queue",
                QueueArgs.builder()
                    .tokenId(tokenId)
                    .tokenSecret(tokenSecret)
                    .name(queueName)
                    .build());

            ctx.export("secretId", secret.secretId());
            ctx.export("volumeId", volume.volumeId());
            ctx.export("queueId", queue.queueId());
        });
    }
}
