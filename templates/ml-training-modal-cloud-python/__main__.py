"""
ML training pipeline — Modal

Provisions the durable Modal objects a training pipeline needs: a Secret
carrying the Hugging Face Hub token training Functions use to pull gated
model weights/datasets, a Volume for checkpoint storage that persists
across training runs, and a Queue for dispatching training job requests
to worker Functions.

Your Modal application code looks these up by name at runtime:

    secret = modal.Secret.from_name("training-secrets")
    volume = modal.Volume.from_name("training-checkpoints")
    queue = modal.Queue.from_name("training-jobs")
"""
import pulumi
import pulumi_labs_modal as modal

config = pulumi.Config()

hf_token = config.require_secret("hfToken")

checkpoint_volume_name = config.get("checkpointVolumeName") or "training-checkpoints"
training_queue_name = config.get("trainingQueueName") or "training-jobs"

# Hugging Face Hub token training Functions use to pull gated model
# weights/datasets.
secret = modal.Secret(
    "secret",
    name="training-secrets",
    values={
        "HF_TOKEN": hf_token,
    },
)

# Durable checkpoint storage that persists across training runs.
volume = modal.Volume(
    "volume",
    name=checkpoint_volume_name,
    version="v2",
)

# Queue a training-job dispatcher enqueues onto; worker Functions pull
# from it to run training jobs asynchronously.
queue = modal.Queue(
    "queue",
    name=training_queue_name,
)

pulumi.export("secret_id", secret.secret_id)
pulumi.export("volume_id", volume.volume_id)
pulumi.export("queue_id", queue.queue_id)
