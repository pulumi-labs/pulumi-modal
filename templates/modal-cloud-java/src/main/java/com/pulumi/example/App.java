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
            var secretName = config.require("secretName");
            var volumeName = config.require("volumeName");
            var queueName = config.require("queueName");

            var secret = new Secret("secret",
                SecretArgs.builder()
                    .name(secretName)
                    .values(Map.of("API_KEY", "changeme"))
                    .build());

            var volume = new Volume("volume",
                VolumeArgs.builder()
                    .name(volumeName)
                    .build());

            var queue = new Queue("queue",
                QueueArgs.builder()
                    .name(queueName)
                    .build());

            ctx.export("secretId", secret.secretId());
            ctx.export("volumeId", volume.volumeId());
            ctx.export("queueId", queue.queueId());
        });
    }
}
