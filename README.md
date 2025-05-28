# Haloy
Haloy is a tool for managing Dockerized applications with zero downtime deployments on your own infrastructure. It uses HAProxy to route traffic to the correct containers based on domain names and provides a configuration file for easy setup.

## Installation

Download the latest release from [GitHub Releases](https://github.com/ameistad/haloy/releases).

```bash
# Linux (AMD64)
curl -L https://github.com/ameistad/haloy/releases/latest/download/haloy-linux-amd64 -o haloy
chmod +x haloy
sudo mv haloy /usr/local/bin/

# macOS (Apple Silicon)
curl -L https://github.com/ameistad/haloy/releases/latest/download/haloy-darwin-arm64 -o haloy
chmod +x haloy
sudo mv haloy /usr/local/bin/
```

## Getting Started

### Prerequisites

- Docker installed and running
- A non-root user added to the docker group: `sudo usermod -aG docker $(whoami)`
- Verify your group membership (you should see “docker” in the output):
  ```bash
  id -nG $(whoami)
  # or
  groups $(whoami)
  ```
- Log out and log back in for group changes to take effect, or run newgrp docker
- Check that you can run Docker commands without sudo:
  ```bash
  docker ps
  ```

### Initialize Haloy

```bash
haloy init
```

This will:
- Set up the directory structure at `~/.config/haloy/`
- Create a sample configuration file at `~/.config/haloy/apps.yml`
- Initialize HAProxy

### Configure Your Apps

Edit the configuration file at `~/.config/haloy/apps.yml`:

Example configuration:
```yaml
apps:
  - name: "example-app"
    source:
      dockerfile:
         path: "/path/to/your/Dockerfile"
         buildContext: "/path/to/your/app"
         buildArgs:
           - "ARG1=value1"
           - "ARG2=value2"
    domains:
      - domain: "example.com"
        aliases:
          - "www.example.com"
      - domain: "api.example.com"
    port: 8080 # Optional: Default is 80
    env: # Optional
      - name: "NODE_ENV"
        value: "production"
      - name: "API_KEY" 
        secretName: "api-key" # Reference to a secret stored with 'haloy secrets set'
   maxContainersToKeep: 5 # Optional: Default is 3
   volumes: # Optional
      - "/host/path:/container/path"
   healthCheckPath: "/health" # Optional: Default is "/"
```

### Deploy Your Apps

```bash
# Deploy a single app
haloy deploy example-app

# Deploy all apps
haloy deploy-all

# Check the status of your deployments
haloy status

# List all deployed containers
haloy list

# Roll back to a previous deployment
haloy rollback example-app
```

## Configuration
### App Configuration

Each app in the `apps` array can have the following properties:

- `name`: Unique name for the app (required)
- `domains`: List of domains for the app (required) 
  - With aliases: `{ domain: "example.com", aliases: ["www.example.com"] }`
- `dockerfile`: Path to your Dockerfile (required)
- `buildContext`: Build context directory for Docker (required)
- `env`: Environment variables for the container
  - Plain values: `{ name: "NODE_ENV", value: "production" }`
  - Secret values: `{ name: "API_KEY", secretName: "api-key" }` (references a stored secret)
- `maxContainersToKeep`: Number of old containers to keep after deployment (default: 3)
- `volumes`: Docker volumes to mount
- `healthCheckPath`: HTTP path for health checks (default: "/")

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

* `haloy.role`: Identifies the role of the container. For applications managed by Haloy and configured in HAProxy, this should be set to `app`.
* `haloy.appName`: (Required) The unique name of your application. This is used to group container instances and name HAProxy backends.
* `haloy.deployment-id`: (Required) An identifier for a specific deployment version of your application (e.g., a timestamp). This helps Haloy manage different versions and is crucial for zero-downtime deployments and rollbacks.
* `haloy.port`: (Required, defaults to "80" if not specified in app config) The port your application container listens on. HAProxy will forward traffic to this port on the container's IP address.
* `haloy.health-check-path`: (Required, defaults to "/" if not specified in app config) The HTTP path on your application that Haloy (and HAProxy) can use to check its health.
* `haloy.acme.email`: (Required) The email address used for ACME (Let's Encrypt) to obtain SSL/TLS certificates for the domains associated with this application. These certificates are then used by HAProxy for HTTPS termination.
* `haloy.domain.<index>`: (Required, e.g., `haloy.domain.0`) The canonical domain name for a set of hostnames serving your application (e.g., `example.com`). HAProxy uses this for host-based ACLs (Access Control Lists) to route traffic.
* `haloy.domain.<index>.alias.<alias_index>`: (Optional, e.g., `haloy.domain.0.alias.0`) Defines an alias for the canonical domain at the same `<index>` (e.g., `www.example.com`). HAProxy will typically be configured to redirect traffic from these aliases to the canonical domain over HTTPS.

This mechanism allows Haloy to dynamically adapt the HAProxy configuration without manual intervention as applications are deployed, updated, or scaled.

**Example in `apps.yml` leading to these labels:**

When you define an application in your `apps.yml` like this:

```yaml
apps:
  - name: "my-web-app"
    source:
      # ... source definition
    domains:
      - domain: "myapp.example.com"
        aliases:
          - "[www.myapp.example.com](https://www.myapp.example.com)"
    acmeEmail: "user@example.com"
    port: "3000"
    healthCheckPath: "/status"
    # ... other app config
```
Haloy will (upon deployment) create containers with labels similar to:

* `haloy.role`: app
* `haloy.appName`: my-web-app
* `haloy.deployment-id`: 20250528210000 (example timestamp)
* `haloy.port`: 3000
* `haloy.health-check-path`: /status
* `haloy.acme.email`: user@example.com
* `haloy.domain.0`: myapp.example.com
* `haloy.domain.0.alias.0`: www.myapp.example.com

These labels are then read by the Haloy manager to dynamically generate the HAProxy configuration, ensuring traffic is routed correctly and securely to your application instances.


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

If you need to build releases manually:

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
