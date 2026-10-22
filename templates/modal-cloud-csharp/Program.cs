using Pulumi;
using Pulumi.Labs.Modal;
using System.Collections.Generic;

return await Deployment.RunAsync(() =>
{
    var config = new global::Pulumi.Config();
    var secretName = config.Get("secretName") ?? "app-secrets";
    var volumeName = config.Get("volumeName") ?? "app-volume";
    var queueName = config.Get("queueName") ?? "app-queue";

    var secret = new Secret("secret", new SecretArgs
    {
        Name = secretName,
        Values = new InputMap<string>
        {
            ["API_KEY"] = "changeme",
        },
    });

    var volume = new Volume("volume", new VolumeArgs
    {
        Name = volumeName,
    });

    var queue = new Queue("queue", new QueueArgs
    {
        Name = queueName,
    });

    return new Dictionary<string, object?>
    {
        ["secretId"] = secret.SecretId,
        ["volumeId"] = volume.VolumeId,
        ["queueId"] = queue.QueueId,
    };
});
