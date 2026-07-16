import * as pulumi from "@pulumi/pulumi";
import * as modal from "@pulumi/modal";

const config = new pulumi.Config();

// Leaving these blank falls back to the modal:tokenId / modal:tokenSecret
// provider config, which itself falls back to the MODAL_TOKEN_ID /
// MODAL_TOKEN_SECRET environment variables.
const tokenId = config.getSecret("tokenId");
const tokenSecret = config.getSecret("tokenSecret");

const secretName = config.get("secretName") ?? "app-secrets";
const volumeName = config.get("volumeName") ?? "app-volume";
const queueName = config.get("queueName") ?? "app-queue";

// Secrets mount as environment variables into Modal Functions.
const secret = new modal.Secret("secret", {
    tokenId,
    tokenSecret,
    name: secretName,
    values: {
        API_KEY: "changeme",
    },
});

// Volumes are durable network filesystems Modal Functions can mount for
// persistent storage.
const volume = new modal.Volume("volume", {
    tokenId,
    tokenSecret,
    name: volumeName,
});

// Queues are durable FIFO/LIFO queues for dispatching work between
// Modal Functions.
const queue = new modal.Queue("queue", {
    tokenId,
    tokenSecret,
    name: queueName,
});

export const secretId = secret.secretId;
export const volumeId = volume.volumeId;
export const queueId = queue.queueId;
