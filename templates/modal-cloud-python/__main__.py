import pulumi
import pulumi_modal as modal

config = pulumi.Config()

secret_name = config.get("secretName") or "app-secrets"
volume_name = config.get("volumeName") or "app-volume"
queue_name = config.get("queueName") or "app-queue"

# Secrets mount as environment variables into Modal Functions.
secret = modal.Secret(
    "secret",
    name=secret_name,
    values={
        "API_KEY": "changeme",
    },
)

# Volumes are durable network filesystems Modal Functions can mount for
# persistent storage.
volume = modal.Volume(
    "volume",
    name=volume_name,
)

# Queues are durable FIFO/LIFO queues for dispatching work between
# Modal Functions.
queue = modal.Queue(
    "queue",
    name=queue_name,
)

pulumi.export("secret_id", secret.secret_id)
pulumi.export("volume_id", volume.volume_id)
pulumi.export("queue_id", queue.queue_id)
