# Haloy

Haloy is a simple and lightweight CLI tool for deploying your apps to a server that you control.

## ✨ Features
  * **Zero-Downtime Deployments:** Haloy waits for new containers to be healthy before switching traffic, ensuring your application is always available.
* **Automatic TLS:** Provides free, automatically renewing TLS certificates from Let's Encrypt.
* **Easy Rollbacks:** Instantly revert to any previous deployment with a single command.
* **Simple Horizontal Scaling:** Scale your application by changing a single number in your config to run multiple container instances.
* **High-performance Reverse Proxy:** Leverages [HAProxy](https://www.haproxy.org/) for load balancing, HTTPS termination, and HTTP/2 support.
* **Deploy from Anywhere:** Integrated API allows for remote deployments from your local machine or a CI/CD pipeline.

## 🚀 Quick Start

### Prerequisites
- A server with root access and a public IP address 
- Docker installed on your server
- Docker image for your app

### 1. Install and Initialize the Haloyd Daemon (haloyd) on Your Server

1. Install `haloyadm`:
    ```bash
    sudo curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-haloyadm.sh | bash
    ```

2. Initialize `haloyd` and `HAProxy`:
    ```bash
    sudo haloyadm init
    ```

    💡 **Optional**: Secure the Haloy API with a domain during initialization:
    ```bash
    sudo haloyadm init --api-domain haloy.yourserver.com --acme-email you@email.com
    ```

    If you don't have a domain ready now, you can set this later with:
    ```bash
    sudo haloyadm api domain haloy.yourserver.com you@email.com
    ```

> [!NOTE]
> For development or non-root installations, you can install in user mode:
> ```bash
> # Install to user directory
> curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-haloyadm.sh | bash
> 
> # Add your user to the docker group (required for non-root Docker access)
> sudo usermod -aG docker $(whoami)
> 
> # Log out and back in, or run:
> newgrp docker
> 
> # Test Docker access
> docker ps
> 
> # Initialize in local mode
> haloyadm init --local-install
> ```
>
> ⚠️ Non-root installations require your user to be in the `docker` group. Adding your user to the `docker` group gives root-equivalent access, so only do this for trusted users.
### 2. Install `haloy` Client
The next step is to install the `haloy` CLI tool that will interact with the haloy server.

1. Install `haloy`
    ```bash
    curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-haloy.sh | bash
    ```

2. Ensure `~/.local/bin` is in your PATH:
    ```bash
    export PATH="$HOME/.local/bin:$PATH"
    ```
    Add this to your `~/.bashrc`, `~/.zshrc`, or equivalent shell profile.

3. Add your server:
    ```bash
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

### 4. Deploy

```bash
haloy deploy

# Check status
haloy status
```

## Architecture

Haloy consists of several components:

1. **Haloy CLI (`haloy`)** - Command-line interface for deployments
1. **Haloy Admin CLI** (`haloyadm`) - Command-line interface to administrate haloyd and secrets.
1. **Haloy Daemon (haloyd)** - Service discovery and configuration management
1. **HAProxy** - Load balancer and SSL termination
1. **Application Containers** - Your deployed applications

The system uses Docker labels for service discovery and dynamic configuration generation.

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
| `deployments_to_keep` | integer | No | Deployment history to keep (default: 6) |
| `pre_deploy` | array | No | Commands to run before deploy |
| `post_deploy` | array | No | Commands to run after deploy |
| `network_mode` | string | No | The Docker network mode for the container. Defaults to Haloy's private network (`haloy-public`) |

#### Image Configuration

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `repository` | string | **Yes** | Docker image name |
| `tag` | string | No | Image tag (default: "latest") |
| `registry` | object | No | Private registry authentication |
| `source` | string | No | Set to "local" for images already on server |

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

## Commands

```bash
# Deploy application
haloy deploy [config-path]

# Check status
haloy status [config-path]

# Stop application containers
haloy stop [config-path]

# Validate configuration file
haloy validate-config [config-path]

# List available rollback targets
haloy rollback-targets [config-path]

# Rollback to specific deployment
haloy rollback [config-path] <deployment-id>

# Manage secrets
haloy secrets set <name> <value>
haloy secrets list
haloy secrets delete <name>

# Roll secrets with haloyadm (creates new encryption key and re-encrypts all existing secrets)
sudo haloyadm secrets roll
```

### Build Locally With the `pre_deploy` hook.
To get up and running quickly with your app you can build the images locally on your own system and upload with scp to your server. Make sure to set the right platform flag for the server you are using and upload the finished image to the server. 

Here's a simple configuration illustrating how we can build and deploy without needing a Docker registry.

Note that we need to add source: local to the image configuration to indicate that we don't need to pull from a registry.
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
  "acmeEmail": "acme@my-app-com",
  "preDeploy": [
    "docker build --platform linux/amd64 -t my-app .",
    "docker save -o my-app.tar my-app",
    "scp my-app.tar $(whoami)@server-ip:/tmp/my-app.tar",
    "ssh $(whoami)@hermes \"docker load -i /tmp/my-app.tar && rm /tmp/my-app.tar\"",
    "rm my-app.tar"
  ]
}
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

Haloy checks for API tokens in this order:

1. **App config**: `api_token_env` field in your `haloy.yaml`
2. **Client config**: Tokens stored via `haloy server add`

### Managing Servers

```bash
# Get your API token from the server
sudo haloyadm api token

# Add a server
haloy server add api.haloy.dev <your-api-token>

# List servers (shows which tokens are available)
haloy server list

# Remove a server
haloy server delete api.haloy.dev
```

### How It Works

`haloy server add` creates two files:

**`~/.config/haloy/client.yaml`** - Server references:
```yaml
servers:
  "api.haloy.dev":
    token_env: "HALOY_TOKEN_API_HALOY_DEV"
```

**`~/.config/haloy/.env`** - Actual tokens:
```bash
HALOY_TOKEN_API_HALOY_DEV=abc123token456
```

When you deploy, Haloy:
1. Loads `.env` files from current directory and config directory
2. Gets server URL from your config
3. Looks up the token environment variable
4. Makes authenticated API calls

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

**Multiple environments:**
```bash
# staging.haloy.yaml
api_token_env: "STAGING_TOKEN"

# production.haloy.yaml  
api_token_env: "PROD_TOKEN"
```

**CI/CD per-project:**
```bash
export PROJECT_A_TOKEN="token_a"
export PROJECT_B_TOKEN="token_b"

haloy deploy project-a/haloy.yaml  # Uses PROJECT_A_TOKEN
haloy deploy project-b/haloy.yaml  # Uses PROJECT_B_TOKEN
```

### Security

- ✅ `.env` files have `0600` permissions (owner only)
- ✅ Config files contain no secrets
- ✅ Works with environment variables or `.env` files

## License

[MIT License](LICENSE)
