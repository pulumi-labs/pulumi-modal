import * as pulumi from "@pulumi/pulumi";
import * as modal from "@pulumi/modal";

const config = new pulumi.Config();
const tokenId = config.requireSecret("tokenId");
const tokenSecret = config.requireSecret("tokenSecret");

// Secrets are durable, named key/value collections that Modal mounts as
// environment variables into your Functions at runtime. Values can be
// rotated in place without replacing the Secret itself.
const apiKeys = new modal.Secret("api-keys", {
    tokenId,
    tokenSecret,
    name: "api-keys",
    values: {
        STRIPE_API_KEY: "sk_test_placeholder",
        SENDGRID_API_KEY: "SG.placeholder",
    },
});

// Volumes are durable, distributed filesystems that Modal Functions can
// mount to persist large artifacts — model weights, checkpoints, datasets —
// across container restarts and between separate Function invocations.
const modelCache = new modal.Volume("model-cache", {
    tokenId,
    tokenSecret,
    name: "model-cache",
    version: "v2",
});

// Queues are durable FIFO/LIFO queues Modal Functions use to pass work to
// one another (e.g. a web endpoint enqueues a job that a pool of GPU
// worker Functions consumes asynchronously).
const jobQueue = new modal.Queue("job-queue", {
    tokenId,
    tokenSecret,
    name: "job-queue",
});

// The built-in Pulumi resource ID (opaque, provider-assigned identifier).
export const secretId = apiKeys.id;
export const volumeId = modelCache.id;
export const queueId = jobQueue.id;

// Modal's own semantic identifiers for these objects, as returned by the
// Modal API — use these when referencing the resources from Modal client
// code or the Modal CLI/dashboard.
export const modalSecretId = apiKeys.secretId;
export const modalVolumeId = modelCache.volumeId;
export const modalQueueId = jobQueue.queueId;
