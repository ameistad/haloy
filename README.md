<p align="center">
    <picture>
      <source srcset="images/haloy-logo.svg" media="(prefers-color-scheme: light)">
      <source srcset="images/haloy-logo-dark.svg" media="(prefers-color-scheme: dark)">      <img src="images/haloy-logo.svg" width="150" alt="Haloy logo">
    </picture>
</p>
<h1 align="center">Haloy</h1>
<p align="center">Deploy containerized apps with zero downtime, automatic SSL, and effortless scaling.</p>

Haloy is a developer-friendly deployment platform that makes deploying Docker containers to your own servers as simple as `git push`. No complex orchestration and no vendor lock-in.

```bash
# Deploy in 3 commands:
haloy server add my-server.com <token>  # Connect to your server
haloy deploy                            # Deploy your app
haloy status                            # Check deployment status
```
**Zero Learning Curve**: If you know Docker, you know Haloy.


## 🚀 Quick Start (5 minutes)

### Prerequisites
- **Server**: Any Linux server with Docker installed
- **Local**: Docker for building your app
- **Domain**: To access the API remotely 

### 1. Install and Initialize the Haloyd Daemon (haloyd) on Your Servers

Repeat these steps for each server you want to deploy to:

1. Install `haloyadm`:
    ```bash
    curl -fsSL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-haloyadm.sh | sudo bash
    ```

2. Initialize `haloyd` and `HAProxy`:
    ```bash
    sudo haloyadm init
    ```

    💡 **Optional**: Secure the Haloy API with a domain during initialization:
    ```bash
    sudo haloyadm init --api-domain haloy.yourserver.com --acme-email you@email.com

    # If you don't have a domain now ready, you can set this later with:
    sudo haloyadm api domain haloy.yourserver.com you@email.com
    ```

> [!NOTE]
> For development or non-root installations, you can install in [user mode](#non-root-install).

### 2. Install `haloy` Client
The next step is to install the `haloy` CLI tool that will interact with the haloy server.

1. Install `haloy`
```bash
curl -fsSL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-haloy.sh | bash
```

2. Ensure `~/.local/bin` is in your PATH:
```bash
export PATH="$HOME/.local/bin:$PATH"
```
    Add this to your `~/.bashrc`, `~/.zshrc`, or equivalent shell profile.

3. Add server:
```bash
# Aad a single server
haloy server add haloy.yourserver.com <api-token>
``` 
> [!TIP]
> See [Authentication & Token Management](#authentication--token-management) for more options on how to manage API tokens.

### 3. Create `haloy.yaml`
In your application's project directory, create a `haloy.yaml` file:

```yaml
server: haloy.yourserver.com
name: "my-app"

# Docker image
image:
  repository: "ghcr.io/your-username/my-app"
  tag: "latest"

# Domain configuration
domains:
  - domain: "my-app.com"
    aliases:
      - "www.my-app.com" # Redirects to my-app.com

# Email for Let's Encrypt registration
acme_email: "you@email.com"
```

For all available options, see the full [Configuration Options](#configuration-options) table below.

For mor information on how Haloy works check out the (architecture section)[#architecture]
## Multi-Server Deployments

Haloy supports multi-server deployments, allowing you to define multiple deployment targets within a single configuration file. Common use cases include:

- **Multi-environment deployments**: Deploy to production, staging, and development environments
- **Geographic distribution**: Deploy to multiple regions with geo-based load balancing  
- **A/B testing**: Deploy different versions to separate infrastructure

```yaml
name: "my-app"
# Base configuration inherited by all targets
image:
  repository: "ghcr.io/your-username/my-app"
  tag: "latest"
acme_email: "you@email.com"

# Global hooks run once regardless of number of targets
global_pre_deploy:
  - "echo 'Starting deployment pipeline'"
  - "npm run build"

global_post_deploy:
  - "echo 'All deployments completed'"

targets:
  production:
    server: production.haloy.com
    image:
      tag: "v1.2.3"  # Override with stable release
    domains:
      - domain: "my-app.com"
    replicas: 3
    env:
      - name: "NODE_ENV"
        value: "production"
  
  staging:
    server: staging.haloy.com
    image:
      tag: "main"  # Use latest main branch
    domains:
      - domain: "staging.my-app.com"
    replicas: 1
    env:
      - name: "NODE_ENV"
        value: "staging"
  
  us-east:
    server: us-east.haloy.com
    domains:
      - domain: "us-api.my-app.com"
    replicas: 2
    env:
      - name: "REGION"
        value: "us-east-1"
```

**Deploy to specific targets:**
```bash
# Deploy to a specific target
haloy deploy --target production
haloy deploy -t us-east

# Deploy to all targets
haloy deploy --all

# Without flags, you'll be prompted to choose
haloy deploy  # Shows available targets for selection
```

**Other commands support target selection:**
```bash
# Check status of specific target
haloy status --target production

# View logs from staging
haloy logs --target staging

# Rollback production only
haloy rollback --target production <deployment-id>

# Stop all targets
haloy stop --all
```

### Separate Configuration Files
You can also use separate configuration files for different environments:

**production.haloy.yaml:**
```yaml
server: production.haloy.com
name: "my-app"
image:
  repository: "ghcr.io/your-username/my-app"
  tag: "v1.2.3"
domains:
  - domain: "my-app.com"
acme_email: "you@email.com"
replicas: 3
```

Deploy using specific configuration files:
```bash
haloy deploy production.haloy.yaml
haloy deploy staging.haloy.yaml
```


**Other commands support target flags too:**
```bash
# Check status of specific target
haloy status --target production

# View logs from staging
haloy logs --target staging

# Rollback production only
haloy rollback --target production <deployment-id>

# Stop all targets
haloy stop --all
```

### Environment-Specific Configuration Files
You can still use separate configuration files for different environments:

**production.haloy.yaml:**
```yaml
server: production.haloy.com
name: "my-app"
image:
  repository: "ghcr.io/your-username/my-app"
  tag: "v1.2.3"
domains:
  - domain: "my-app.com"
acme_email: "you@email.com"
replicas: 3
```

**staging.haloy.yaml:**
```yaml
server: staging.haloy.com
name: "my-app-staging"
image:
  repository: "ghcr.io/your-username/my-app"
  tag: "main"
domains:
  - domain: "staging.my-app.com"
acme_email: "you@email.com"
replicas: 1
```

Deploy to different environments:
```bash
haloy deploy production.haloy.yaml
haloy deploy staging.haloy.yaml
```

### 4. Deploy

```bash
haloy deploy

# Check status
haloy status
```

## Architecture

Haloy manages several components working together:

1. **Haloy CLI (`haloy`)** - Command-line interface for deployments and management from your local machine
2. **Haloy Admin CLI (`haloyadm`)** - Command-line interface to set up and administrate `haloyd` and HAProxy
3. **Haloy Daemon (`haloyd`)** - Service discovery, configuration management, and container orchestration
4. **HAProxy** - External load balancer providing SSL termination and traffic routing (managed by Haloy)
5. **Application Containers** - Your deployed applications orchestrated by `haloyd`

The system uses Docker labels for service discovery and dynamic HAProxy configuration generation. `haloyd` continuously monitors your application containers and automatically updates HAProxy's configuration to route traffic appropriately.

## Configuration Reference

### Format Support
Haloy supports YAML, JSON, and TOML formats:
- **YAML/TOML**: Use `snake_case` (e.g., `acme_email`)
- **JSON**: Use `camelCase` (e.g., `acmeEmail`)

### Configuration Options

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `name` | string | **Yes** | Unique application name |
| `image` | object | **Yes** | Docker image configuration |
| `server` | string | No | Haloy server API URL |
| `api_token_env` | string | No | Environment variable containing API token (see [Set Token In App Configuration](#set-token-in-app-configuration)) |
| `domains` | array | No | Domain configuration |
| `acme_email` | string | No | Let's Encrypt email (required with domains) |
| `replicas` | integer | No | Number of container instances (default: 1) |
| `port` | string | No | Container port (default: "8080") |
| `health_check_path` | string | No | Health check endpoint (default: "/") |
| `env` | array | No | Environment variables (see [Environment Variables](#environment-variables)) |
| `volumes` | array | No | Volume mounts |
| `pre_deploy` | array | No | Commands to run before deploy |
| `post_deploy` | array | No | Commands to run after deploy |
| `global_pre_deploy` | array | No | Commands to run once before all deployments (multi-target only) |
| `global_post_deploy` | array | No | Commands to run once after all deployments (multi-target only) |
| `targets` | object | No | Multiple deployment targets with overrides (see [Multi-Target Deployments](#multi-target-deployments-new)) |
| `network_mode` | string | No | The Docker network mode for the container. Defaults to Haloy's private network (`haloy-public`) |

#### Image Configuration

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `repository` | string | **Yes** | Docker image name |
| `tag` | string | No | Image tag (default: "latest") |
| `registry` | object | No | Private registry authentication |
| `source` | string | No | Where the source for the image is. If set to local it will only look for images already on the server. (default: registry) |
| `history` | object | No | Image history and rollback strategy (see [Image History](#image-history)) |

#### Target Configuration

When using multi-target deployments, each target can override any of the base configuration options:

| Key | Type | Description |
|-----|------|-------------|
| `server` | string | Override the server for this target |
| `api_token_env` | string | Override the API token environment variable |
| `image` | object | Override image configuration (repository, tag, etc.) |
| `domains` | array | Override domain configuration |
| `acme_email` | string | Override ACME email |
| `env` | array | Override environment variables |
| `replicas` | integer | Override number of replicas |
| `port` | string | Override container port |
| `health_check_path` | string | Override health check path |
| `volumes` | array | Override volume mounts |
| `deployments_to_keep` | integer | **Deprecated**: Override deployment history count (use `image.history.count` instead) |
| `pre_deploy` | array | Override pre-deploy hooks |
| `post_deploy` | array | Override post-deploy hooks |
| `network_mode` | string | Override network mode |

**Target Inheritance Rules:**
- Base configuration provides defaults for all targets
- Target-specific values completely override base values (no merging)
- Only specified fields in targets override the base; unspecified fields use base values
- `global_pre_deploy` and `global_post_deploy` run once regardless of targets
- Individual target `pre_deploy` and `post_deploy` run for each target deployment

#### Environment Variables

Environment variables can be configured in two ways:

**1. Plain text values:**
```yaml
env:
  - name: "DATABASE_URL"
    value: "postgres://localhost:5432/myapp"
  - name: "DEBUG"
    value: "true"
```

**2. Secret references:** (requires [secrets management](#secrets-management))
```yaml
env:
  - name: "API_KEY"
    secret_name: "my-api-key"
  - name: "DATABASE_PASSWORD"
    secret_name: "db-password"
```

**Environment Variable Configuration:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | **Yes** | Environment variable name |
| `value` | string | No* | Plain text value |
| `secret_name` | string | No* | Reference to a stored secret |

> **Note**: You must provide either `value` OR `secret_name`, but not both.

**Examples:**

<details>
<summary>YAML Format</summary>

```yaml
env:
  - name: "NODE_ENV"
    value: "production"
  - name: "PORT"
    value: "3000"
  - name: "API_SECRET"
    secret_name: "app-api-secret"
```
</details>

<details>
<summary>JSON Format</summary>

```json
{
  "env": [
    {
      "name": "NODE_ENV",
      "value": "production"
    },
    {
      "name": "PORT", 
      "value": "3000"
    },
    {
      "name": "API_SECRET",
      "secretName": "app-api-secret"
    }
  ]
}
```
</details>

<details>
<summary>TOML Format</summary>

```toml
[[env]]
name = "NODE_ENV"
value = "production"

[[env]]
name = "PORT"
value = "3000"

[[env]]
name = "API_SECRET"
secret_name = "app-api-secret"
```
</details>

**Security Best Practices:**
- ✅ Use `secret_name` for sensitive values (passwords, API keys, tokens)
- ✅ Use `value` for non-sensitive configuration (ports, URLs, feature flags)
- ❌ Never put sensitive data in `value` fields as they're stored in plain text

#### Image History

Haloy supports different strategies for managing image history and rollbacks:

| Strategy | Description | Use Case |
|----------|-------------|----------|
| `local` | Keep images locally (default) | Fast rollbacks, local development |
| `registry` | Rely on registry tags | Save disk space, production with versioned releases |
| `none` | No rollback support | Minimal storage, no rollback needs |

**Image History Configuration:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `strategy` | string | No | History strategy: "local", "registry", or "none" (default: "local") |
| `count` | integer | Conditional* | Number of images/deployments to keep (required for "local" and "registry") |
| `pattern` | string | Conditional* | Tag pattern for registry rollbacks (required for "registry" strategy) |

> **Note**: `count` is required for "local" and "registry" strategies. `pattern` is required for "registry" strategy.

**Examples:**

<details>
<summary>Local Strategy (Default)</summary>

```yaml
name: "my-app"
image:
  repository: "ghcr.io/my-org/my-app"
  tag: "latest"
  history:
    strategy: "local"
    count: 5  # Keep 5 images locally
domains:
  - domain: "my-app.com"
```
</details>

<details>
<summary>Registry Strategy</summary>

```yaml
name: "my-app"
image:
  repository: "ghcr.io/my-org/my-app"
  tag: "v1.2.3"  # Must use immutable tags
  history:
    strategy: "registry"
    count: 10  # Track 10 deployment versions
    pattern: "v*"  # Match versioned tags for rollbacks
domains:
  - domain: "my-app.com"
```
</details>

<details>
<summary>No History Strategy</summary>

```yaml
name: "my-app"
image:
  repository: "ghcr.io/my-org/my-app"
  tag: "latest"
  history:
    strategy: "none"  # No rollback support
domains:
  - domain: "my-app.com"
```
</details>

**Strategy Details:**

- **Local Strategy**: Haloy automatically tags images with deployment IDs and keeps them locally. Fast rollbacks but uses more disk space.

- **Registry Strategy**: Relies on your registry's existing tags for rollbacks. You must use immutable tags (no "latest", "main", etc.). Saves local disk space but requires proper tagging discipline.

- **None Strategy**: Disables rollback capability entirely. Minimal resource usage but no rollback safety net.

#### Target Inheritance Example

```yaml
name: "my-app"
# Base configuration - inherited by all targets
image:
  repository: "ghcr.io/my-org/my-app"
  tag: "latest"
replicas: 2
port: "8080"
env:
  - name: "LOG_LEVEL"
    value: "info"
  - name: "FEATURE_FLAG"
    value: "false"

targets:
  production:
    # Inherits: replicas=2, port="8080", LOG_LEVEL="info", FEATURE_FLAG="false"
    # Overrides: image.tag, adds domains, overrides env completely
    server: "prod.haloy.com"
    image:
      tag: "v1.2.3"  # Override tag only, repository inherited
    domains:
      - domain: "my-app.com"
    env:  # Completely replaces base env - no LOG_LEVEL or FEATURE_FLAG inherited
      - name: "NODE_ENV"
        value: "production"
      - name: "DB_URL"
        secret_name: "prod-db-url"
  
  staging:
    # Inherits: image.repository, image.tag="latest", replicas=2, port="8080", env array
    # Overrides: server, adds domains, changes replicas
    server: "staging.haloy.com"
    replicas: 1  # Override replicas
    domains:
      - domain: "staging.my-app.com"
    # env not specified - inherits base env with LOG_LEVEL and FEATURE_FLAG
```

## Commands

### Deployment Commands
```bash
# Deploy application
haloy deploy [config-path]
haloy deploy --target production      # Deploy to specific target
haloy deploy -t staging              # Short form
haloy deploy --all                   # Deploy to all targets
haloy deploy --no-logs              # Skip deployment logs

# Check status
haloy status [config-path]
haloy status --target production     # Status for specific target
haloy status --all                   # Status for all targets

# Stop application containers
haloy stop [config-path]
haloy stop --target production       # Stop specific target
haloy stop --all                     # Stop all targets

# View logs
haloy logs [config-path]
haloy logs --target staging          # Logs from specific target

# Validate configuration file
haloy validate-config [config-path]

# List available rollback targets
haloy rollback-targets [config-path]
haloy rollback-targets --target production

# Rollback to specific deployment
haloy rollback [config-path] <deployment-id>
haloy rollback --target production <deployment-id>

# Note: Rollback availability depends on image.history.strategy:
# - local: Fast rollbacks from locally stored images
# - registry: Rollbacks use registry tags (requires immutable tags)
# - none: No rollback support
```

### Server Management Commands
```bash
# Add a server
haloy server add <url> <token>

# List configured servers
haloy server list

# Remove a server
haloy server delete <url>
```

### Secrets Management Commands
```bash
# Manage secrets
haloy secrets set <name> <value>
haloy secrets set --target production <name> <value>  # Set for specific target
haloy secrets set --all <name> <value>               # Set for all targets

haloy secrets list
haloy secrets list --target staging                  # List from specific target
haloy secrets list --all                            # List from all targets

haloy secrets delete <name>
haloy secrets delete --target production <name>      # Delete from specific target
haloy secrets delete --all <name>                   # Delete from all targets

# Roll secrets with haloyadm (creates new encryption key and re-encrypts all existing secrets)
sudo haloyadm secrets roll
```

### Build Locally With Deployment Hooks
To get up and running quickly with your app you can build the images locally on your own system and upload with scp to your server. Make sure to set the right platform flag for the server you are using and upload the finished image to the server. 

Here's a simple configuration illustrating how we can build and deploy without needing a Docker registry.

Note that we need to add source: local to the image configuration to indicate that we don't need to pull from a registry.

**Single Target Example:**
```json
{
  "server": "haloy.yourserver.com",
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
  "acmeEmail": "acme@my-app.com",
  "preDeploy": [
    "docker build --platform linux/amd64 -t my-app .",
    "docker save -o my-app.tar my-app",
    "scp my-app.tar $(whoami)@server-ip:/tmp/my-app.tar",
    "ssh $(whoami)@server-ip \"docker load -i /tmp/my-app.tar && rm /tmp/my-app.tar\"",
    "rm my-app.tar"
  ]
}
```

**Multi-Target Example with Global Hooks:**
```yaml
name: "my-app"
image:
  repository: "my-app"
  source: "local"
  tag: "latest"

# Build once, deploy to multiple servers
global_pre_deploy:
  - "docker build --platform linux/amd64 -t my-app ."
  - "docker save -o my-app.tar my-app"

global_post_deploy:
  - "rm my-app.tar"  # Cleanup after all deployments

targets:
  production:
    server: "prod.haloy.com"
    domains:
      - domain: "my-app.com"
    pre_deploy:
      - "scp my-app.tar $(whoami)@prod-server-ip:/tmp/my-app.tar"
      - "ssh $(whoami)@prod-server-ip \"docker load -i /tmp/my-app.tar && rm /tmp/my-app.tar\""
  
  staging:
    server: "staging.haloy.com"
    domains:
      - domain: "staging.my-app.com"
    pre_deploy:
      - "scp my-app.tar $(whoami)@staging-server-ip:/tmp/my-app.tar"
      - "ssh $(whoami)@staging-server-ip \"docker load -i /tmp/my-app.tar && rm /tmp/my-app.tar\""
```

## Horizontal Scaling

Scale your application by setting the `replicas` field:

```yaml
name: "my-scalable-app"
image:
  repository: "my-org/my-app"
  tag: "1.2.0"
replicas: 3  # Run 3 instances
domains:
  - domain: "api.example.com"
acme_email: "you@email.com"
```

## Uninstalling

### Remove Client Only
```bash
curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/uninstall-haloy.sh | bash
```

### Remove Admin Tool Only
```bash
curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/uninstall-haloyadm.sh | bash
```

### Complete Server Removal
```bash
curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/uninstall-server.sh | bash
```

## Directory Structure

Haloy uses standard system directories:

**System Installation (default):**
```
/etc/haloy/              # Configuration
├── haloyd.yaml          # Haloyd settings
├── .env                 # API tokens

/var/lib/haloy/          # Data
├── haproxy-config/      # HAProxy configs
├── cert-storage/        # SSL certificates
└── db/                  # Database files
```

**User Installation (`--local-install`):**
```
~/.config/haloy/         # Configuration
~/.local/share/haloy/    # Data
```

## Authentication & Token Management

Haloy supports managing multiple servers, each with their own API tokens. Haloy checks for API tokens in this order:

1. **App config**: `api_token_env` field in your `haloy.yaml`
2. **Client config**: Tokens stored via `haloy server add`

### Managing Multiple Servers

```bash
# Get API tokens from each server
sudo haloyadm api token

# Add multiple servers with their tokens
haloy server add production.haloy.com <production-token>
haloy server add staging.haloy.com <staging-token>
haloy server add dev.haloy.com <dev-token>

# List all configured servers and their token status
haloy server list

# Remove a server
haloy server delete staging.haloy.com
```

### How It Works

When you run `haloy server add`, Haloy creates two files:

**`~/.config/haloy/client.yaml`** - Server references:
```yaml
servers:
  "production.haloy.com":
    token_env: "HALOY_API_TOKEN_PRODUCTION_HALOY_COM"
  "staging.haloy.com":
    token_env: "HALOY_API_TOKEN_STAGING_HALOY_COM"
```

**`~/.config/haloy/.env`** - Actual tokens:
```bash
HALOY_API_TOKEN_PRODUCTION_HALOY_COM=abc123token456
HALOY_API_TOKEN_STAGING_HALOY_COM=def789token012
```

When you deploy, Haloy:
1. Loads `.env` files from current directory and config directory
2. Gets server URL from your config
3. Looks up the corresponding token environment variable
4. Makes authenticated API calls to the specified server

### Server Selection Priority

Haloy determines which server to deploy to using this priority order:

1. **Explicit server in config**: `server: production.haloy.com` in your haloy.yaml
2. **Single server auto-selection**: If only one server is configured, it's used automatically
3. **Error for multiple servers**: If multiple servers are configured but none specified in config, Haloy will list available servers and prompt you to specify one

### Set Token In App Configuration

An alternative option is to set the API token in the app configuration file:

```yaml
name: "my-app"
server: "api.haloy.dev"
api_token_env: "PRODUCTION_DEPLOY_TOKEN"  # Use this token instead
image:
  repository: "my-app"
  tag: "latest"
```

Set the token in your environment:
```bash
export PRODUCTION_DEPLOY_TOKEN="your_token_here"
```

### Use Cases

**Multiple environments with different servers:**
```bash
# production.haloy.yaml
server: production.haloy.com
api_token_env: "PROD_TOKEN"

# staging.haloy.yaml  
server: staging.haloy.com
api_token_env: "STAGING_TOKEN"

# Deploy to different environments
export PROD_TOKEN="production_token_here"
export STAGING_TOKEN="staging_token_here"

haloy deploy production.haloy.yaml
haloy deploy staging.haloy.yaml
```

**CI/CD with multiple projects and servers:**
```bash
# Each project can have its own server and token
export PROJECT_A_PROD_TOKEN="token_a_prod"
export PROJECT_A_STAGING_TOKEN="token_a_staging"
export PROJECT_B_TOKEN="token_b"

# project-a/production.haloy.yaml
server: project-a-prod.haloy.com
api_token_env: "PROJECT_A_PROD_TOKEN"

# project-a/staging.haloy.yaml
server: project-a-staging.haloy.com
api_token_env: "PROJECT_A_STAGING_TOKEN"

# project-b/haloy.yaml
server: project-b.haloy.com
api_token_env: "PROJECT_B_TOKEN"
```

**Single server with multiple projects:**
```bash
# All projects deploy to the same server but with different app names
# app1.haloy.yaml
server: shared.haloy.com
name: "app1"

# app2.haloy.yaml
server: shared.haloy.com
name: "app2"
```

### Security

- ✅ `.env` files have `0600` permissions (owner only)
- ✅ Config files contain no secrets
- ✅ Works with environment variables or `.env` files

### Non-root install
```bash
# Install to user directory
curl -fsSL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-haloyadm.sh | bash

# Add your user to the docker group (required for non-root Docker access)
sudo usermod -aG docker $(whoami)

# Log out and back in, or run:
newgrp docker

# Test Docker access
docker ps

# Initialize in local mode
haloyadm init --local-install
```

⚠️ Non-root installations require your user to be in the `docker` group. Adding your user to the `docker` group gives root-equivalent access, so only do this for trusted users.

## License

[MIT License](LICENSE)
