package com.pulumi.example;

import com.pulumi.Pulumi;
import com.pulumi.labs.modal.AppArgs;
import com.pulumi.labs.modal.Dict;
import com.pulumi.labs.modal.DictArgs;
import com.pulumi.labs.modal.Environment;
import com.pulumi.labs.modal.EnvironmentArgs;
import com.pulumi.labs.modal.Image;
import com.pulumi.labs.modal.ImageArgs;
import com.pulumi.labs.modal.ModalFunctions;
import com.pulumi.labs.modal.inputs.GetEnvironmentArgs;
import java.util.List;
import java.util.Map;

public class App {
    public static void main(String[] args) {
        Pulumi.run(ctx -> {
            var config = ctx.config();
            var environmentName = config.get("environmentName").orElse("platform");
            var appName = config.get("appName").orElse("platform-app");
            var dictName = config.get("dictName").orElse("platform-state");
            var imageName = config.get("imageName").orElse("platform-base");

            // The environment is the namespace every other Modal object lives in; cap its concurrent tasks.
            var env = new Environment("env",
                EnvironmentArgs.builder()
                    .name(environmentName)
                    .maxConcurrentTasks(20)
                    .build());

            // The App groups Functions and is placed in the environment above; passing env.name() makes the dependency implicit.
            var app = new com.pulumi.labs.modal.App("app",
                AppArgs.builder()
                    .name(appName)
                    .environment(env.name())
                    .tags(Map.of("team", "ml", "managedBy", "pulumi"))
                    .build());

            // A Dict is a durable key-value store that Functions in this environment share.
            var state = new Dict("state",
                DictArgs.builder()
                    .name(dictName)
                    .environment(env.name())
                    .build());

            // A published Image: a slim Python base plus a pinned dependency, built once and reused by Functions.
            var base = new Image("base",
                ImageArgs.builder()
                    .baseImage("python:3.12-slim")
                    .dockerfileCommands(List.of("RUN pip install numpy==2.1.3"))
                    .name(imageName)
                    .environment(env.name())
                    .build());

            // Look up the workspace the provider's token belongs to, then its default environment.
            var workspace = ModalFunctions.getWorkspace();
            var defaultEnv = ModalFunctions.getEnvironment(GetEnvironmentArgs.builder()
                .name(workspace.applyValue(w -> w.defaultEnvironment()))
                .build());

            ctx.export("environmentId", env.environmentId());
            ctx.export("appId", app.appId());
            ctx.export("dictId", state.dictId());
            ctx.export("imageId", base.imageId());
            ctx.export("workspaceName", workspace.applyValue(w -> w.workspaceName()));
            ctx.export("defaultEnvironmentWebSuffix", defaultEnv.applyValue(e -> e.webSuffix()));
        });
    }
}
