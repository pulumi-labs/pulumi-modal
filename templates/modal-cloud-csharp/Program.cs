using Pulumi;
using Pulumi.Modal;
using System.Collections.Generic;

return await Deployment.RunAsync(() =>
{
    var config = new global::Pulumi.Config();
    var tokenId = config.RequireSecret("tokenId");
    var tokenSecret = config.RequireSecret("tokenSecret");
    var secretName = config.Get("secretName") ?? "app-secrets";
    var volumeName = config.Get("volumeName") ?? "app-volume";
    var queueName = config.Get("queueName") ?? "app-queue";

    var secret = new Secret("secret", new SecretArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = secretName,
        Values = new InputMap<string>
        {
            ["API_KEY"] = "changeme",
        },
    });

    var volume = new Volume("volume", new VolumeArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = volumeName,
    });

    var queue = new Queue("queue", new QueueArgs
    {
        TokenId = tokenId,
        TokenSecret = tokenSecret,
        Name = queueName,
    });

    return new Dictionary<string, object?>
    {
        ["secretId"] = secret.SecretId,
        ["volumeId"] = volume.VolumeId,
        ["queueId"] = queue.QueueId,
    };
});
