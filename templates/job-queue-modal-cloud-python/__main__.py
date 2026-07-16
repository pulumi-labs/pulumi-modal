"""
Background job queue — Modal

Provisions a Modal Queue that a web app or scheduler enqueues jobs onto,
plus a Secret carrying the API key background worker Functions use to
authenticate callbacks back to your application.

Your Modal application code looks these up by name at runtime:

    queue = modal.Queue.from_name("background-jobs")
    secret = modal.Secret.from_name("worker-callback-secret")
"""
import pulumi
import pulumi_modal as modal

config = pulumi.Config()

token_id = config.get_secret("tokenId")
token_secret = config.get_secret("tokenSecret")
queue_name = config.get("queueName") or "background-jobs"
worker_api_key = config.require_secret("workerApiKey")

# API key background workers use to authenticate callbacks to your
# application once a job completes.
secret = modal.Secret(
    "secret",
    token_id=token_id,
    token_secret=token_secret,
    name="worker-callback-secret",
    values={
        "WORKER_API_KEY": worker_api_key,
    },
)

# Durable queue a dispatcher enqueues jobs onto; a pool of worker
# Functions pulls from it to process them asynchronously.
queue = modal.Queue(
    "queue",
    token_id=token_id,
    token_secret=token_secret,
    name=queue_name,
)

pulumi.export("secret_id", secret.secret_id)
pulumi.export("queue_id", queue.queue_id)
