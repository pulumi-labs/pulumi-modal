// ML training pipeline — Modal
//
// Provisions the durable Modal objects a training pipeline needs: a
// Secret carrying the Hugging Face Hub token training Functions use to
// pull gated model weights/datasets, a Volume for checkpoint storage that
// persists across training runs, and a Queue for dispatching training job
// requests to worker Functions.
//
// Your Modal application code looks these up by name at runtime:
//
//   secret = modal.Secret.from_name("training-secrets")
//   volume = modal.Volume.from_name("training-checkpoints")
//   queue = modal.Queue.from_name("training-jobs")
using Pulumi;
using Pulumi.Modal;
using System.Collections.Generic;

return await Deployment.RunAsync(() =>
{
    var config = new global::Pulumi.Config();
    var tokenId = config.GetSecret("tokenId");
    var tokenSecret = config.GetSecret("tokenSecret");
    var hfToken = config.RequireSecret("hfToken");

    var checkpointVolumeName = config.Get("checkpointVolumeName") ?? "training-checkpoints";
    var trainingQueueName = config.Get("trainingQueueName") ?? "training-jobs";

    // Hugging Face Hub token training Functions use to pull gated model
    // weights/datasets.
    var secret = new Secret("secret", new SecretArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = "training-secrets",
        Values = new InputMap<string>
        {
            ["HF_TOKEN"] = hfToken,
        },
    });

    // Durable checkpoint storage that persists across training runs.
    var volume = new Volume("volume", new VolumeArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = checkpointVolumeName,
        Version = "v2",
    });

    // Queue a training-job dispatcher enqueues onto; worker Functions
    // pull from it to run training jobs asynchronously.
    var queue = new Queue("queue", new QueueArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = trainingQueueName,
    });

    return new Dictionary<string, object?>
    {
        ["secretId"] = secret.SecretId,
        ["volumeId"] = volume.VolumeId,
        ["queueId"] = queue.QueueId,
    };
});
