# Haloy
Haloy is a free tool for zero‑downtime Docker app deploys with automatic HAProxy routing and TLS certificates.

## ✨ Features
* 🐳 Deploy and rollback any application using Docker.
* 🔄 High-performance reverse proxy leveraging [HAProxy](https://www.haproxy.org/). 
* 🔒 Automatic obtain and renew SSL/TLS certificates
* 💻 Straightforward command-line interface.
* 🌐 HTTP API for automation, integration, and remote management.

## Requirements
- Docker installed and running
- Your user is part of the docker group. This lets you run Haloy and Docker commands without sudo.
    - Add your user: `sudo usermod -aG docker $(whoami)`
    - Verify (you should see "docker"): `id -nG $(whoami)` or `groups $(whoami)`
    - Important: Log out and log back in for the group change to take effect, or run `newgrp docker` in your current shell.
    - Test it: `docker ps` (should work without sudo).
- Haloy has been tested on Debian 12, but should work on most systems. 

> [!NOTE]
> Adding your user to the `docker` group gives it root-equivalent access to Docker. Only do this for trusted users. If you prefer you can skip this step and run Haloy with `sudo` (e.g., `sudo haloy init`).
## Get Started
These are the steps to install Haloy on the server where you are hosting your dockerized applications. 

## Install

### Server Install
Run this on the server you plan on running Haloy:
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

### Client install (optional for remote deploys)
Run this on your local development machine or any machine that only needs to deploy applications to the server. It installs only the `haloy` client.

```bash
curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install.sh | bash
```

After installation, ensure `~/.local/bin` is in your `PATH`:
```bash
export PATH="$HOME/.local/bin:$PATH"
```


## Deploying Apps


### Quickstart
To get up and running quickly with your app you can build the images locally on your own system. Make sure to set the right platform flag for the server you are using and upload the finished image to the server. Here's a simple bash script you can use for the pre deploy hook.

### Configuration
Create a haloy.yaml file. TODO: add full list of config options with explanation.


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
- `source`: (Required) Defines where to get your application's Docker image. See the [Source Configuration](#source) below.
- `domains`: (Required) A list of domains for the app. Can be a simple list of strings or a map with aliases. Aliases will automatically be redirected to the main (canonical) domain.
  - Example:
    ```yaml
    domains:
    - domain: "example.com"
      aliases:
        - "www.example.com"
    - domain: "api.example.com"
    ```
- `acmeEmail`: (Required) The email address to use for Let's Encrypt TLS certificate registration.
- `env`: (Optional) Environment variables for the container
  - Example:
    ```yaml
      env:
        - name: "NODE_ENV"
          value: "production" # plain text value
        - name: "API_SECRET_KEY"
          secretName: "my-secret-key" # Reference to a secret. 
      ```
- `deploymentsToKeep`: (Optional) Number of old deployment data (images and config) to keep for rollbacks (default: 5)
- `port`: (Optional) The port your application container listens on. Haloy needs this to configure HAProxy correctly. (Default: "80")
- `replicas`: (Optional) The number of container instances to run for this application. When thera are more than one replicas, Haloy starts multiple identical containers and automatically configures HAProxy to distribute traffic between them using round-robin load balancing. (Default: 1)
- `volumes`: Docker volumes to mount
- `healthCheckPath`: (Optional) The HTTP path that Haloy will check for a 2xx status code to determine if your application is healthy. This is used as a fallback if you don't define a HEALTHCHECK instruction in your Dockerfile. (Default: "/")


## Hooks
Haloy has pre deploy and post deploy hooks which will execute commands on the machine you are running `haloy`. 

TODO: documents how hooks work.


## How Rollbacks Work

Haloy provides robust rollback capabilities, allowing you to revert your application to a previous, stable state. This is achieved by leveraging historical Docker images and stored application configurations.

### Deployment History
Whenever a new application is successfully deployed, Haloy performs the following actions:

1.  **Image Tagging**: The Docker image used for the deployment is tagged with a unique `deployment-id`. This ID is a timestamp in `YYYYMMDDHHMMSS` format (e.g., `20250615214304`), ensuring chronological order and uniqueness. For example, an image `my-app:latest` deployed at a specific time might also be tagged as `my-app:20250615214304`.
2.  **Configuration Snapshot**: A snapshot of the application's configuration (`AppConfig`) at the time of deployment is saved to a history folder, named after the `deployment-id` (e.g., `~/.config/haloy/history/20250615214304.yml`). This ensures that not only the image but also the exact configuration (domains, environment variables, health checks, etc.) from that specific deployment is preserved.
3.  **Automatic Cleanup**: To prevent excessive disk usage, Haloy automatically prunes old deployment history, keeping only the most recent `N` deployments as configured by `deploymentsToKeep` (default: 5). Similarly, old Docker image tags (excluding `:latest` and those in use by running containers) are removed for images associated with older deployments.

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
