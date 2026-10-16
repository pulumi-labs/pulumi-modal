// Background job queue — Modal
//
// Provisions a Modal Queue that a web app or scheduler enqueues jobs
// onto, plus a Secret carrying the API key background worker Functions
// use to authenticate callbacks back to your application.
//
// Your Modal application code looks these up by name at runtime:
//
//   queue = modal.Queue.from_name("background-jobs")
//   secret = modal.Secret.from_name("worker-callback-secret")
using Pulumi;
using Pulumi.Modal;
using System.Collections.Generic;

return await Deployment.RunAsync(() =>
{
    var config = new global::Pulumi.Config();
    var queueName = config.Get("queueName") ?? "background-jobs";
    var workerApiKey = config.RequireSecret("workerApiKey");

    // API key background workers use to authenticate callbacks to your
    // application once a job completes.
    var secret = new Secret("secret", new SecretArgs
    {
        Name = "worker-callback-secret",
        Values = new InputMap<string>
        {
            ["WORKER_API_KEY"] = workerApiKey,
        },
    });

    // Durable queue a dispatcher enqueues jobs onto; a pool of worker
    // Functions pulls from it to process them asynchronously.
    var queue = new Queue("queue", new QueueArgs
    {
        Name = queueName,
    });

    return new Dictionary<string, object?>
    {
        ["secretId"] = secret.SecretId,
        ["queueId"] = queue.QueueId,
    };
});
