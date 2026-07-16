using Pulumi;
using Pulumi.Modal;
using System.Collections.Generic;

return await Deployment.RunAsync(() =>
{
    var config = new global::Pulumi.Config();
    var tokenId = config.RequireSecret("tokenId");
    var tokenSecret = config.RequireSecret("tokenSecret");

    // Secrets mount as environment variables into Modal Functions at runtime.
    var secret = new Secret("api-keys", new SecretArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = "api-keys",
        Values = new InputMap<string>
        {
            ["STRIPE_API_KEY"] = "sk_live_xxx_placeholder",
            ["SENDGRID_API_KEY"] = "SG.xxx_placeholder",
        },
    });

    // Volumes are durable network filesystems that Modal Functions mount for shared, persistent state (e.g. model weights).
    var volume = new Volume("model-cache", new VolumeArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = "model-cache",
        Version = "v2",
    });

    // Queues are durable FIFO/LIFO dispatch queues used to coordinate work between Modal Functions.
    var queue = new Queue("job-queue", new QueueArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = "job-queue",
    });

    return new Dictionary<string, object?>
    {
        ["secretId"] = secret.SecretId,
        ["volumeId"] = volume.VolumeId,
        ["queueId"] = queue.QueueId,
    };
});
