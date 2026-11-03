import pulumi
import pulumi_labs_modal as modal

# An Environment is a Modal namespace that scopes Apps, Dicts and Images.
# Capping concurrent tasks keeps a runaway workload from exhausting the workspace.
env = modal.Environment(
    "env",
    name="platform",
    max_concurrent_tasks=20,
)

# An App groups Functions and is the unit you deploy and tag. Referencing
# env.name makes Pulumi create the Environment first.
app = modal.App(
    "app",
    name="platform-app",
    environment=env.name,
    tags={
        "team": "ml",
        "managedBy": "pulumi",
    },
)

# A Dict is a durable key/value store that Functions share across invocations.
state = modal.Dict(
    "state",
    name="platform-state",
    environment=env.name,
)

# An Image is built from a registry base plus Dockerfile commands and published
# under a stable name that Functions can reference.
base = modal.Image(
    "base",
    base_image="python:3.12-slim",
    dockerfile_commands=["RUN pip install numpy==2.1.3"],
    name="platform-base",
    environment=env.name,
)

# Lookups: the workspace behind the token and its default Environment.
workspace = modal.get_workspace_output()
default_env = modal.get_environment_output(name=workspace.default_environment)

pulumi.export("environmentId", env.environment_id)
pulumi.export("appId", app.app_id)
pulumi.export("dictId", state.dict_id)
pulumi.export("imageId", base.image_id)
pulumi.export("workspaceName", workspace.workspace_name)
pulumi.export("defaultEnvironmentWebSuffix", default_env.web_suffix)
