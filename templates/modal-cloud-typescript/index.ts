import * as pulumi from "@pulumi/pulumi";
import * as modal from "@pulumi-labs/modal";

const config = new pulumi.Config();

const secretName = config.get("secretName") ?? "app-secrets";
const volumeName = config.get("volumeName") ?? "app-volume";
const queueName = config.get("queueName") ?? "app-queue";

// Secrets mount as environment variables into Modal Functions.
const secret = new modal.Secret("secret", {
    name: secretName,
    values: {
        API_KEY: "changeme",
    },
});

// Volumes are durable network filesystems Modal Functions can mount for
// persistent storage.
const volume = new modal.Volume("volume", {
    name: volumeName,
});

// Queues are durable FIFO/LIFO queues for dispatching work between
// Modal Functions.
const queue = new modal.Queue("queue", {
    name: queueName,
});

export const secretId = secret.secretId;
export const volumeId = volume.volumeId;
export const queueId = queue.queueId;
