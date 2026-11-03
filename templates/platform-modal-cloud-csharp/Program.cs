using Pulumi;
using Pulumi.Labs.Modal;
using System.Collections.Generic;

return await Deployment.RunAsync(() =>
{
    var config = new global::Pulumi.Config();
    var environmentName = config.Get("environmentName") ?? "platform";
    var appName = config.Get("appName") ?? "platform-app";
    var dictName = config.Get("dictName") ?? "platform-state";
    var imageName = config.Get("imageName") ?? "platform-base";

    // The Environment is an isolated namespace in the Modal workspace that scopes Apps, Dicts and Images.
    var env = new Pulumi.Labs.Modal.Environment("env", new EnvironmentArgs
    {
        Name = environmentName,
        MaxConcurrentTasks = 20,
    });

    // The App groups Functions and is placed in the environment above; using env.Name makes the dependency implicit.
    var app = new App("app", new AppArgs
    {
        Name = appName,
        Environment = env.Name,
        Tags = new InputMap<string>
        {
            ["team"] = "ml",
            ["managedBy"] = "pulumi",
        },
    });

    // The Dict is a durable key-value store shared between Modal Functions.
    var state = new Dict("state", new DictArgs
    {
        Name = dictName,
        Environment = env.Name,
    });

    // The Image is built from a slim Python base plus extra Dockerfile commands and published under a name.
    var image = new Image("base", new ImageArgs
    {
        BaseImage = "python:3.12-slim",
        DockerfileCommands = new InputList<string>
        {
            "RUN pip install numpy==2.1.3",
        },
        Name = imageName,
        Environment = env.Name,
    });

    // Look up the workspace, then the environment it uses by default.
    var workspace = GetWorkspace.Invoke();
    var defaultEnvironment = GetEnvironment.Invoke(new GetEnvironmentInvokeArgs
    {
        Name = workspace.Apply(w => w.DefaultEnvironment),
    });

    return new Dictionary<string, object?>
    {
        ["environmentId"] = env.EnvironmentId,
        ["appId"] = app.AppId,
        ["dictId"] = state.DictId,
        ["imageId"] = image.ImageId,
        ["workspaceName"] = workspace.Apply(w => w.WorkspaceName),
        ["defaultEnvironmentWebSuffix"] = defaultEnvironment.Apply(e => e.WebSuffix),
    };
});
