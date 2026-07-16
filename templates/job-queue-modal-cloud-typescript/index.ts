/**
 * Background job queue — Modal
 *
 * Provisions a Modal Queue that a web app or scheduler enqueues jobs
 * onto, plus a Secret carrying the API key background worker Functions
 * use to authenticate callbacks back to your application.
 *
 * Your Modal application code looks these up by name at runtime:
 *
 *   queue = modal.Queue.from_name("background-jobs")
 *   secret = modal.Secret.from_name("worker-callback-secret")
 */
import * as pulumi from "@pulumi/pulumi";
import * as modal from "@pulumi/modal";

const config = new pulumi.Config();

const tokenId = config.getSecret("tokenId");
const tokenSecret = config.getSecret("tokenSecret");
const queueName = config.get("queueName") ?? "background-jobs";
const workerApiKey = config.requireSecret("workerApiKey");

// API key background workers use to authenticate callbacks to your
// application once a job completes.
const secret = new modal.Secret("secret", {
    tokenId,
    tokenSecret,
    name: "worker-callback-secret",
    values: {
        WORKER_API_KEY: workerApiKey,
    },
});

// Durable queue a dispatcher enqueues jobs onto; a pool of worker
// Functions pulls from it to process them asynchronously.
const queue = new modal.Queue("queue", {
    tokenId,
    tokenSecret,
    name: queueName,
});

export const secretId = secret.secretId;
export const queueId = queue.queueId;
