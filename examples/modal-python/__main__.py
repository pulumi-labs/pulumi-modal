import pulumi
import pulumi_labs_modal as modal

# Secrets are durable, named key/value collections that Modal mounts as
# environment variables into your Functions at runtime. Values can be
# rotated in place without replacing the Secret itself.
api_keys = modal.Secret(
    "api-keys",
    name="api-keys",
    values={
        "STRIPE_API_KEY": "sk_test_xxx_placeholder",
        "SENDGRID_API_KEY": "SG.placeholder",
    },
)

# Volumes are durable, distributed filesystems that Modal Functions can
# mount to persist large artifacts — model weights, checkpoints, datasets —
# across container restarts and between separate Function invocations.
model_cache = modal.Volume(
    "model-cache",
    name="model-cache",
    version="v2",
)

# Queues are durable FIFO/LIFO queues Modal Functions use to pass work to
# one another (e.g. a web endpoint enqueues a job that a pool of GPU
# worker Functions consumes asynchronously).
job_queue = modal.Queue(
    "job-queue",
    name="job-queue",
)

# The built-in Pulumi resource ID (opaque, provider-assigned identifier).
pulumi.export("secret_id", api_keys.id)
pulumi.export("volume_id", model_cache.id)
pulumi.export("queue_id", job_queue.id)

# Modal's own semantic identifiers for these objects, as returned by the
# Modal API — use these when referencing the resources from Modal client
# code or the Modal CLI/dashboard.
pulumi.export("modal_secret_id", api_keys.secret_id)
pulumi.export("modal_volume_id", model_cache.volume_id)
pulumi.export("modal_queue_id", job_queue.queue_id)
