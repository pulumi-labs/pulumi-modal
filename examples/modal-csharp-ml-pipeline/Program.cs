using Pulumi;
using Pulumi.Modal;
using System.Collections.Generic;

// This example provisions the shared infrastructure backing an ML training and
// inference pipeline on Modal: a Secret holding model-hub and API credentials,
// a Volume for durable checkpoint storage, and a Queue for dispatching training
// job requests. The Modal Functions that actually run the training/inference
// code (defined separately in the user's Modal app, typically Python) reference
// these resources at runtime via modal.Secret.from_name("inference-secrets"),
// modal.Volume.from_name("checkpoint-storage"), and
// modal.Queue.from_name("training-jobs") respectively.
return await Deployment.RunAsync(() =>
{
    var config = new global::Pulumi.Config();
    var tokenId = config.RequireSecret("tokenId");
    var tokenSecret = config.RequireSecret("tokenSecret");

    // Credentials Modal Functions read at runtime via modal.Secret.from_name("inference-secrets").
    var secret = new Secret("inference-secrets", new SecretArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = "inference-secrets",
        Values = new InputMap<string>
        {
            ["HF_TOKEN"] = "hf_placeholder_token",
            ["OPENAI_API_KEY"] = "sk_placeholder",
        },
    });

    // Durable model checkpoint storage mounted via modal.Volume.from_name("checkpoint-storage").
    var checkpoints = new Volume("checkpoint-storage", new VolumeArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = "checkpoint-storage",
        Version = "v2",
    });

    // Dispatch queue for training job requests, consumed via modal.Queue.from_name("training-jobs").
    var trainingJobs = new Queue("training-jobs", new QueueArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = "training-jobs",
    });

    return new Dictionary<string, object?>
    {
        ["secretId"] = secret.SecretId,
        ["checkpointVolumeId"] = checkpoints.VolumeId,
        ["trainingQueueId"] = trainingJobs.QueueId,
    };
});
