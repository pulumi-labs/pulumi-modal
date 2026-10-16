/**
 * ML training pipeline — Modal
 *
 * Provisions the durable Modal objects a training pipeline needs: a
 * Secret carrying the Hugging Face Hub token training Functions use to
 * pull gated model weights/datasets, a Volume for checkpoint storage that
 * persists across training runs, and a Queue for dispatching training job
 * requests to worker Functions.
 *
 * Your Modal application code looks these up by name at runtime:
 *
 *   secret = modal.Secret.from_name("training-secrets")
 *   volume = modal.Volume.from_name("training-checkpoints")
 *   queue = modal.Queue.from_name("training-jobs")
 */
import * as pulumi from "@pulumi/pulumi";
import * as modal from "@pulumi/modal";

const config = new pulumi.Config();

const hfToken = config.requireSecret("hfToken");

const checkpointVolumeName = config.get("checkpointVolumeName") ?? "training-checkpoints";
const trainingQueueName = config.get("trainingQueueName") ?? "training-jobs";

// Hugging Face Hub token training Functions use to pull gated model
// weights/datasets.
const secret = new modal.Secret("secret", {
    name: "training-secrets",
    values: {
        HF_TOKEN: hfToken,
    },
});

// Durable checkpoint storage that persists across training runs.
const volume = new modal.Volume("volume", {
    name: checkpointVolumeName,
    version: "v2",
});

// Queue a training-job dispatcher enqueues onto; worker Functions pull
// from it to run training jobs asynchronously.
const queue = new modal.Queue("queue", {
    name: trainingQueueName,
});

export const secretId = secret.secretId;
export const volumeId = volume.volumeId;
export const queueId = queue.queueId;
