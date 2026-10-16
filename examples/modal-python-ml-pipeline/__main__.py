"""
ML training/inference pipeline — Modal

Provisions the three durable Modal objects a training + inference pipeline
typically needs:
  - A Secret holding third-party API credentials used inside training and
    inference Functions (a hosted model API and Hugging Face Hub).
  - A Volume for checkpoint storage that persists across training runs, so
    a run can resume from -- or an inference Function can load -- the
    latest checkpoint without re-downloading it.
  - A Queue for dispatching training job requests to a pool of worker
    Functions.

These resources are provisioned once via Pulumi. Your Modal application
code (deployed separately with `modal deploy`) then looks them up by name
at runtime -- it never needs the Pulumi outputs directly:

    secret = modal.Secret.from_name("inference-secrets")
    volume = modal.Volume.from_name("checkpoint-storage")
    queue = modal.Queue.from_name("training-jobs")

    @app.function(secrets=[secret], volumes={"/checkpoints": volume})
    def train(): ...
"""
import pulumi
import pulumi_modal as modal

# Credentials training and inference Functions need: a Hugging Face Hub
# token to pull gated model weights/datasets, and an OpenAI key used for
# eval scoring during training.
inference_secrets = modal.Secret(
    "inference-secrets",
    name="inference-secrets",
    values={
        "HF_TOKEN": "hf_placeholder",
        "OPENAI_API_KEY": "sk-placeholder",
    },
)

# Durable checkpoint storage shared across training runs. Mounted by the
# training Function to write checkpoints and by inference Functions to
# load the latest trained weights.
checkpoint_storage = modal.Volume(
    "checkpoint-storage",
    name="checkpoint-storage",
    version="v2",
)

# Queue that a training-job dispatcher enqueues onto; a pool of GPU worker
# Functions pulls from it to run training jobs asynchronously.
training_jobs = modal.Queue(
    "training-jobs",
    name="training-jobs",
)

# The built-in Pulumi resource ID (opaque, provider-assigned identifier).
pulumi.export("secret_id", inference_secrets.id)
pulumi.export("volume_id", checkpoint_storage.id)
pulumi.export("queue_id", training_jobs.id)

# Modal's own semantic identifiers for these objects.
pulumi.export("modal_secret_id", inference_secrets.secret_id)
pulumi.export("modal_volume_id", checkpoint_storage.volume_id)
pulumi.export("modal_queue_id", training_jobs.queue_id)
