# Haloy
Haloy is a simple and lightweight CLI tool for deploying your apps to a server that you control. It automates the deployment of Docker containers and manages HAProxy to handle reverse proxying, load balancing, and TLS termination.

## ✨ Features
  * **Zero-Downtime Deployments:** Haloy waits for new containers to be healthy before switching traffic, ensuring your application is always available.
* **Automatic TLS:** Provides free, automatically renewing TLS certificates from Let's Encrypt.
* **Easy Rollbacks:** Instantly revert to any previous deployment with a single command.
* **Simple Horizontal Scaling:** Scale your application by changing a single number in your config to run multiple container instances.
* **High-performance Reverse Proxy:** Leverages [HAProxy](https://www.haproxy.org/) for load balancing, HTTPS termination, and HTTP/2 support.
* **Deploy from Anywhere:** A simple API allows for remote deployments from your local machine or a CI/CD pipeline.


# 🚀 Getting Started: Your First Deploy in 5 Minutes
This guide will walk you through setting up Haloy and deploying a sample application.

__Prerequisites:__
- A server with a public IP address.
- Docker installed on your server.
- You've added your user to the docker group to run commands without sudo. (See instructions below).
- A Docker image for your application pushed to a registry (like Docker Hub or GHCR).

## Install and Initialize Haloy on Your Server
First, SSH into your server.

1. Install the `haloy` and `haloyadm` tools using the install script:
    ```bash
    curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-server.sh | bash
    ```
    You may need to add ~/.local/bin to your server's $PATH.

1. Initialize the Haloy services. This command creates the necessary directories and starts the Haloy Manager and HAProxy containers.
    ```bash
    haloyadm init
    ```
    💡 Optional: If have domain ready, you can secure the Haloy API itself during initialization:
    ```bash
    haloyadm init --api-domain haloy.yourserver.com --acme-email you@email.com
    ```

## Configure Your Local Machine for Remote Deploys
This is optional as you can run the `haloy` command from your server and it will use the API locally. 

1. Install the haloy client tool:
  ```bash
  curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install.sh | bash
  ```

1. Ensure ~/.local/bin is in your PATH. Add this to your ~/.bashrc, ~/.zshrc, or equivalent shell profile:
  ```bash
  export PATH="$HOME/.local/bin:$PATH"?
  ```

## Install
For Haloy to work you need to have Docker installed. It's also recommended that you add your user to the [Docker user group](#add-user-to-docker-group).

Run this on your server to install the `haloy` (deploy) and `haloyadm` (server admin) cli tools:
```bash
curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-server.sh | bash
```

Then run:
```bash
haloyadm init
```

If you want to use the API for remote deployments and you have added DNS records to the server.
```bash
haloyadm init --api-domain api.yourserver.com --acme-email you@youremail.com
```

## Remote deploys (optional)
If you want to trigger deploys from a CI or your own machine you only need the `haloy` cli tool. Install with this command:

```bash
curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install.sh | bash
```

Ensure `~/.local/bin` is in your `PATH`:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

Automatically setup API token and set default server:
```bash
haloy setup ssh user@<ip|host>
```

### Add user to docker group
`sudo usermod -aG docker $(whoami)`

Verify (you should see "docker"): `id -nG $(whoami)` or `groups $(whoami)`

Important: Log out and log back in for the group change to take effect, or run `newgrp docker` in your current shell.

Test it: `docker ps` (should work without sudo).

> [!NOTE]
> Adding your user to the `docker` group gives it root-equivalent access to Docker. Only do this for trusted users. If you prefer you can skip this step and run Haloy with `sudo` (e.g., `sudo haloy init`).


## Building and Configuring


The first step is to create a `haloy.yaml` file (json and toml is also supported).

You can call the file whatever you like, but if you don't use haloy.json,yaml/yml,toml you have to specify what config file to use. For example `haloy deploy my-app.json`

By using a config file it's easy to keep it in version control with the rest of your code. 

__Add configuration the `haloy.yaml` config file__

`haloy.yaml`
```yaml
server: "https://haloy.my-app.com"
name: "my-app"
image:
  repository: "ghcr.io/your-github-user/my-app"
  tag: "latest"
domains:
  - domain: "my-app.com"
    aliases:
      - "www.my-app.com"
      - "blog.my-app.com"
acme_email: "acme@my-app.com"
```

<details>
<summary>haloy.json example</summary>

```json
{
  "server": "https://haloy.my-app.com",
  "name": "my-app",
  "image": {
    "repository": "ghcr.io/your-github-user/my-app",
    "tag": "latest"
  },
  "domains": [
    {
      "domain": "my-app.com",
      "aliases": ["www.my-app.com", "blog.my-app.com"],
    }
  ],
  "acmeEmail": "acme@my-app.com",
}
```

</details>

<details>
<summary>haloy.toml example</summary>

```toml
server = "https://haloy.my-app.com"
name = "my-app"
acmeEmail = "acme@my-app.com"
healthCheckPath = "/health"

[image]
repository = "ghcr.io/your-github-user/my-app"
tag = "latest"

[[domains]]
domain = "my-app.com"
aliases = ["www.my-app.com", "blog.my-app.com"]
```

</details>

[Checkout the full list of configuration options](#configuration-options).

### The configration file
Haloy support `yaml/yml`, `json` and `toml` format for the configuration file. Keep in mind that config options (fields/keys) uses camelCase for json and snake_case for yaml and toml.


You can name the file whatever you want and it can live anywhere as long as the cli tool haloy has access to it. It's often a good idea to keep it in your repo so it's version controlled. 

If you don't provide a path to the file name when running `haloy deploy` it will look for a haloy.yaml in the current directory. 

### Build locally with a hook
To get up and running quickly with your app you can build the images locally on your own system and upload with scp to your server. Make sure to set the right platform flag for the server you are using and upload the finished image to the server. 

Here's a simple configuration illustrating how we can build and deploy without needing a Docker registry.

Note that we need to add source: local to the image configuration to indicate that we don't need to pull from a registry.
```json
  {
  "server": "https://haloy.my-app.com",
  "name": "my-app",
  "image": {
    "repository": "my-app",
    "source": "local",
    "tag": "latest"
  },
  "domains": [
    {
      "domain": "my-app.com"
    }
  ],
  "acmeEmail": "acme@my-app-com",
  "preDeploy": [
    "docker build --platform linux/amd64 -t haloy-demo-buzy .",
    "docker save -o haloy-demo-buzy.tar haloy-demo-buzy",
    "scp haloy-demo-buzy.tar $(whoami)@hermes:/tmp/haloy-demo-buzy.tar",
    "ssh $(whoami)@hermes \"docker load -i /tmp/haloy-demo-buzy.tar && rm /tmp/haloy-demo-buzy.tar\"",
    "rm haloy-demo-buzy.tar"
  ]
}


```

## ⚙️ Configuration Options
Haloy uses a single configuration file for each application. While `haloy.yaml` is the default, you can also use `.json` or `.toml`. Note that YAML and TOML use `snake_case`, while JSON uses `camelCase`.

| Key                 | Type      | Required | Description                                                                                                                                                                     |
|---------------------|-----------|----------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `name`              | `string`  | **Yes** | A unique name for your application. Used for naming containers and HAProxy backends.                                                                                              |
| `image`             | `object`  | **Yes** | Defines the Docker image to be deployed. See [Image Configuration](#image-configuration) below.                                                                                   |
| `server`            | `string`  | No       | The URL of the Haloy manager API. If not set, you must configure it globally with `haloy setup ssh`.                                                                              |
| `domains`           | `array`   | No       | A list of domains for your app. Each item can define a `domain` (canonical) and `aliases`. Required for web traffic.                                                              |
| `acme_email`        | `string`  | No       | The email address for Let's Encrypt registration. Required if `domains` are specified.                                                                                          |
| `replicas`          | `integer` | No       | The number of container instances to run for this application for horizontal scaling. Defaults to `1`.                                                                            |
| `port`              | `string`  | No       | The port your application listens on inside the container. Defaults to `"8080"`.                                                                                                |
| `health_check_path` | `string`  | No       | The HTTP path for health checks (e.g., `/health`). Haloy expects a 2xx status code. Defaults to `/`. Ignored if your Dockerfile has a `HEALTHCHECK` instruction.                 |
| `env`               | `array`   | No       | A list of environment variables. Can be a plaintext `value` or a reference to a `secret_name`.                                                                                    |
| `volumes`           | `array`   | No       | A list of Docker volume mounts in the format `/host/path:/container/path`.                                                                                                      |
| `deployments_to_keep` | `integer` | No       | Number of old deployments (images and configs) to keep for rollbacks. Defaults to `6`.                                                                                          |
| `pre_deploy`        | `array`   | No       | A list of shell commands to execute on the client machine **before** the deployment starts.                                                                                       |
| `post_deploy`       | `array`   | No       | A list of shell commands to execute on the client machine **after** the deployment finishes.                                                                                      |
| `network_mode`      | `string`  | No       | The Docker network mode for the container. Defaults to Haloy's private network (`haloy-public`).                                                                                  |

#### Image Configuration
The `image` object has the following fields:

| Key          | Type     | Required | Description                                                                                                                                                                   |
|--------------|----------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `repository` | `string` | **Yes** | The name of the Docker image (e.g., `ghcr.io/my-user/my-app`).                                                                                                                  |
| `tag`        | `string` | No       | The image tag to use. Defaults to `latest`.                                                                                                                                   |
| `source`     | `string` | No       | Set to `local` if the image is already on the server and should not be pulled from a registry. Defaults to `registry`.                                                         |
| `registry`   | `object` | No       | Authentication details for a private Docker registry. Includes `server`, `username`, and `password`. The credentials themselves can be sourced from env vars, secrets, or plaintext. |

## Deploying





### Deploy commands

```bash
# Deploy app
haloy deploy my-app.yaml

# Check the status
haloy status my-app.yaml

# Roll back to a previous deployment
haloy rollback my-app.yaml 20231026143000
```

For a full list of command run `haloy help`

## Configuration
### App Configuration

Each app in the `apps` list can have the following properties:

- `name`: (Required) Unique name for the app.
- `image`: TODO
- `domains`: (Required) A list of domains for the app. Can be a simple list of strings or a map with aliases. Aliases will automatically be redirected to the main (canonical) domain.
  - Example:
    ```yaml
    domains:
    - domain: "example.com"
      aliases:
        - "www.example.com"
    - domain: "api.example.com"
    ```
- `acme_email`: The email address to use for Let's Encrypt TLS certificate registration.
- `env`: (Optional) Environment variables for the container
  - Example:
    ```yaml
      env:
        - name: "NODE_ENV"
          value: "production" # plain text value
        - name: "API_SECRET_KEY"
          secret_name: "my-secret-key" # Reference to a secret. 
      ```
- `deployments_to_keep`: (Optional) Number of old deployment data (images and config) to keep for rollbacks (default: 5)
- `port`: (Optional) If not set you need to use port 8080 as this is the default. If you use any other port in your container you need to set this.
- `replicas`: (Optional) The number of container instances to run for this application. When thera are more than one replicas, Haloy starts multiple identical containers and automatically configures HAProxy to distribute traffic between them using round-robin load balancing. (Default: 1)
- `volumes`: Docker volumes to mount
- `health_check_path`: (Optional) The HTTP path that Haloy will check for a 2xx status code to determine if your application is healthy. This is used as a fallback if you don't define a HEALTHCHECK instruction in your Dockerfile. (Default: "/")


## Hooks
Haloy has pre deploy and post deploy hooks which will execute commands on the machine you are running `haloy`. 

TODO: documents how hooks work.


## How Rollbacks Work

Haloy provides robust rollback capabilities, allowing you to revert your application to a previous, stable state. This is achieved by leveraging historical Docker images and stored application configurations.

### Performing a Rollback
To initiate a rollback, you use the `haloy rollback` command.

**List Rollback Targets**: If you run `haloy rollback <app-name>` without specifying a `deployment-id`, Haloy will list all available historical deployments for that application. This list includes the `deployment-id`, the `image tag` used, and indicates which one is currently considered "latest".

```bash
haloy rollback my-app
```
This command leverages the stored image tags and optionally the configuration snapshots to show you the previous deployment states.

**Execute Rollback**: To perform the rollback, you provide the `deployment-id` of the specific historical version you wish to restore.

```bash
haloy rollback my-app 20231026143000
```

When a rollback is executed, Haloy uses the image corresponding to the `target-deployment-id`. It also attempts to retrieve the stored `AppConfig` from that historical `deployment-id`. This historical image and configuration are then used to initiate a *new* deployment, complete with a *new* unique `deployment-id`. This ensures that the rollback itself creates a new, trackable deployment in your history, maintaining the integrity of your deployment timeline. The previous running containers for the application (that are not part of the new deployment) will be stopped and removed.

### Secrets Management

Haloy provides a secure way to manage sensitive environment variables using the `secrets` command:

```bash
# Initialize the secrets system (generates encryption keys)
haloy secrets init

# Store a secret
haloy secrets set api-key "your-secret-api-key"

# List all stored secrets
haloy secrets list

# Delete a secret
haloy secrets delete api-key
```

To use a secret in your app configuration:

1. Store the secret using `haloy secrets set <secret-name> <value>`
2. Reference it in your `apps.yml` file:
   ```yaml
   env:
     - name: "API_KEY"
       secretName: "api-key"  # References the stored secret named "api-key"
   ```

Secrets are securely encrypted at rest using [age encryption](https://age-encryption.org) and are only decrypted when needed for deployments.


#### Storing AGE-SECRET-KEY (private key)
By default the key used to decrypt secrets is stored in `~/.config/haloy/age_identity.txt` with strict permissions set. Instead of relying on the ~/.config/haloy/age_identity.txt file, you can provide the private key directly via the `HALOY_AGE_IDENTITY` environment variable.

1. Get your private key. You can display it by reading the identity file:
```bash
cat ~/.config/haloy/age_identity.txt
```

2. Set the `HALOY_AGE_IDENTITY` environment variable in your shell environment with the value of the key.

```bash
export HALOY_AGE_IDENTITY="AGE-SECRET-KEY-1..."
```
Haloy will automatically use this environment variable for decrypting secrets if it is set.

## Preparing Images for Haloy
To ensure smooth, reliable deployments, your application images should be built with a few best practices in mind.

### Health Checks
Haloy checks the health of new containers before finalizing a deployment and switching traffic. This is crucial for achieving zero-downtime deployments. Haloy uses two methods to determine if an application is ready:

__1. Recommended Method. `HEALTHCHECK` in Dockerfile__

The most reliable way to report your application's status is by using the native `HEALTHCHECK` instruction in your Dockerfile. Haloy respects this instruction and will wait for the container's status to become healthy before proceeding with the deployment.

Example Dockerfile:
```dockerfile
FROM node:18-alpine

# ... (rest of your app setup) ...

# This command checks if the app is responding on port 3000 at the /healthz endpoint.
# It should return exit code 0 on success or 1 on failure.
HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:3000/healthz || exit 1

CMD [ "node", "server.js" ]
```

__2. Fallback Method: `healthCheckPath` in `apps.yml`__
If your Docker image does not include a `HEALTHCHECK` instruction, Haloy will fall back to performing an HTTP GET request. It will send the request to the endpoint you specify in the `healthCheckPath` property of your `apps.yml` file. Haloy considers the application healthy if it receives any `2xx` status code in response.

## Horizontal Scaling with Replicas
Haloy supports horizontal scaling out-of-the-box through the replicas property in your apps.yml configuration. By setting this value to more than one, you instruct Haloy to start multiple identical instances of your application's container for a single deployment.

When you run multiple replicas, Haloy automatically configures the HAProxy backend to load balance traffic across all healthy instances of your application. By default, it uses a round-robin strategy to distribute incoming requests evenly. This setup enhances both performance and availability, as traffic will be redirected away from any instance that fails its health check.

This property allows you to easily scale your application to handle more traffic and improve its fault tolerance without any manual configuration of the load balancer. The default is 1 replica if the property is not specified.

__Example__: Here’s how you would configure an application to run with three replicas:
```yaml
apps:
  - name: "my-scalable-app"
    source:
      image:
        repository: "my-org/my-app"
        tag: "1.2.0"
    replicas: 3 # Haloy will start 3 instances of this container
    domains:
      - domain: "api.example.com"
    acmeEmail: "your-email@example.com"
```

## Architecture

Haloy is designed to simplify the deployment and management of Dockerized applications with dynamic HAProxy-based load balancing and automated SSL certificate management. Its architecture comprises several key components:

1.  **Haloy CLI (`haloy`):**
    * The command-line interface used by developers to interact with the Haloy system.
    * Responsibilities: Initializing Haloy, managing application configurations (`apps.yml`), deploying applications, managing secrets, checking status, etc.

2.  **Haloy Manager (daemon):**
    * A long-running daemon (typically run as a Docker container itself, e.g., `ghcr.io/ameistad/haloy-manager`).
    * **Core Responsibilities:**
        * **Service Discovery:** Continuously monitors Docker for application containers managed by Haloy, identified by specific Docker labels.
        * **Dynamic HAProxy Configuration:** Generates and applies HAProxy configurations based on the discovered application containers and their labels. It signals HAProxy to reload its configuration gracefully with zero downtime.
        * **Automated Certificate Management:** Manages SSL/TLS certificates using ACME (Let's Encrypt) for domains specified in application labels. It handles certificate issuance and renewals.
        * **Event Handling:** Responds to Docker events (e.g., container start/stop) to keep the HAProxy configuration up-to-date.

3.  **HAProxy (daemon):**
    * A high-performance TCP/HTTP load balancer.
    * Haloy configures HAProxy to:
        * Route traffic based on hostnames to the appropriate backend application containers.
        * Terminate HTTPS connections using certificates managed by the Haloy Manager.
        * Handle HTTP to HTTPS redirection.
        * Serve ACME HTTP-01 challenges.

4.  **Application Containers:**
    * User-provided Dockerized applications.
    * These containers **must** be deployed with specific Docker labels (see below) for the Haloy Manager to discover and manage them.

### Service Discovery and Configuration via Docker Labels

Docker labels are the cornerstone of how the Haloy Manager identifies and configures services for HAProxy. When you deploy an application through Haloy (based on your `apps.yml` configuration), the resulting containers are automatically assigned these crucial labels.

Haloy uses the following Docker container labels on your application containers:

* `dev.haloy.role`: (Required) Identifies the role of the container. For applications managed by Haloy and configured in HAProxy, this should be set to `app`.
* `dev.haloy.appName`: (Required) The unique name of your application. This is used to group container instances and name HAProxy backends.
* `dev.haloy.deployment-id`: (Required) An identifier for a specific deployment version of your application (e.g., a timestamp). This helps Haloy manage different versions and is crucial for zero-downtime deployments and rollbacks.
* `dev.haloy.port`: (Required, defaults to "80" if not specified in app config) The port your application container listens on. HAProxy will forward traffic to this port on the container's IP address.
* `dev.haloy.health-check-path`: (Required, defaults to "/" if not specified in app config) The HTTP path on your application that Haloy (and HAProxy) can use to check its health.
* `dev.haloy.acme.email`: (Required) The email address used for ACME (Let's Encrypt) to obtain SSL/TLS certificates for the domains associated with this application. These certificates are then used by HAProxy for HTTPS termination.
* `dev.haloy.domain.<index>`: (Required, e.g., `dev.haloy.domain.0`) The canonical domain name for a set of hostnames serving your application (e.g., `example.com`). HAProxy uses this for host-based ACLs (Access Control Lists) to route traffic.
* `dev.haloy.domain.<index>.alias.<alias_index>`: (Optional, e.g., `dev.haloy.domain.0.alias.0`) Defines an alias for the canonical domain at the same `<index>` (e.g., `www.example.com`). HAProxy will typically be configured to redirect traffic from these aliases to the canonical domain over HTTPS.

This mechanism allows Haloy to dynamically adapt the HAProxy configuration without manual intervention as applications are deployed, updated, or scaled.

**Example in `apps.yml` leading to these labels:**

When you define an application in your `apps.yml` like this:

```yaml
apps:
  - name: "my-web-app"
    source:
      # ... source definition isn't defined in labels
    domains:
      - domain: "myapp.example.com"
        aliases:
          - "www.myapp.example.com"
    acmeEmail: "user@example.com"
    port: "3000"
    healthCheckPath: "/status"
    # ... other app config
```
Haloy will (upon deployment) create containers with labels similar to:

* `dev.haloy.role`: app
* `dev.haloy.appName`: my-web-app
* `dev.haloy.deployment-id`: 20250528210000 (example timestamp)
* `dev.haloy.port`: 3000
* `dev.haloy.health-check-path`: /status
* `dev.haloy.acme.email`: user@example.com
* `dev.haloy.domain.0`: myapp.example.com
* `dev.haloy.domain.0.alias.0`: www.myapp.example.com

These labels are then read by the Haloy manager to dynamically generate the HAProxy configuration, ensuring traffic is routed correctly and securely to your application instances.

### Directory Structure

Haloy uses two separate directories to organize its files:

__Configuration Directory (`~/.config/haloy`)__
Used for CLI settings and authentication:
- **`.env`** - API token for connecting to remote haloy-manager instances
- Can be customized with `HALOY_CONFIG_DIR` environment variable

__Data Directory (`~/.local/share/haloy`)__
- Docker Compose files for haloy-manager and HAProxy
- **`haproxy-config`** - Dynamic HAProxy configuration file
- **`cert-storage/`** - SSL certificates and ACME account data
- **`.env`** - Local haloy-manager environment variables
- **`haloy.db`** - SQLite database (future feature)
- Can be customized with `HALOY_DATA_DIR` environment variable


## Development

### Building the CLI

```bash
go build -o haloy ./cmd/cli
```

## Releasing

Haloy uses GitHub Actions for automated builds and releases.

### Automated Release Process

1. Make sure all code changes are committed and pushed to main:
   - Any workflow or CI/CD changes should be made separately from the release process
   - Push these changes and wait for the workflow to complete before proceeding

2. Update the version number in relevant files:
   ```bash
   # Edit the version constant in internal/version/version.go
   git add internal/version/version.go
   git commit -m "Bump version to v1.0.0"
   git push origin main
   ```

3. Create an annotated tag for the release:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0: Brief description of changes"
   git push origin v1.0.0
   ```

4. GitHub Actions will automatically:
   - Run all tests
   - Build the manager Docker image and push it to GitHub Container Registry with version tags
   - Build the CLI binaries for all supported platforms
   - Create a GitHub Release with the binaries attached

5. Verify the release at:
   ```
   https://github.com/ameistad/haloy/releases
   ```

**Important Note**: The workflow is optimized to:
- Only run tests for pushes to branches and pull requests
- Run the full build process (including release) only when pushing a tag
- This separation prevents duplicate builds and conserves GitHub Actions minutes

### Manual Release Process

1. Build the CLI for multiple platforms:
   ```bash
   # Linux (AMD64)
   GOOS=linux GOARCH=amd64 go build -o haloy-linux-amd64 ./cmd/cli
   
   # Linux (ARM64)
   GOOS=linux GOARCH=arm64 go build -o haloy-linux-arm64 ./cmd/cli
   
   # macOS (AMD64)
   GOOS=darwin GOARCH=amd64 go build -o haloy-darwin-amd64 ./cmd/cli
   
   # macOS (ARM64)
   GOOS=darwin GOARCH=arm64 go build -o haloy-darwin-arm64 ./cmd/cli
   
   # Windows
   GOOS=windows GOARCH=amd64 go build -o haloy-windows-amd64.exe ./cmd/cli
   ```

2. Build and push the manager Docker image:
   ```bash
   docker build -t ghcr.io/ameistad/haloy-manager:latest -f build/manager/Dockerfile .
   docker push ghcr.io/ameistad/haloy-manager:latest
   ```
## License

[MIT License](LICENSE)
