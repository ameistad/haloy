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

### 1. Install and Initialize the Haloy Manager on Your Server

1. Install `haloyadm` (system-wide installation):
    ```bash
    sudo curl -sL https://raw.githubusercontent.com/ameistad/haloy/main/scripts/install-haloyadm.sh | bash
    ```

2. Initialize the `haloy-manager` and `HAProxy`:
    ```bash
    sudo haloyadm init
    ```

    💡 **Optional**: Secure the Haloy API with a domain during initialization:
    ```bash
    sudo haloyadm init --api-domain haloy.yourserver.com --acme-email you@email.com
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

3. Connect to your server:
    ```bash
    haloy setup haloy.yourserver.com <api-token>
    ``` 

### 3. Create `haloy.yaml`
In your application's project directory, create a `haloy.yaml` file:

```yaml
# Server URL (optional if running haloy on the server)
server: haloy.yourserver.com

# Unique name for your application
name: "my-app"

# Docker image to deploy
image:
  repository: "ghcr.io/your-username/my-app"
  tag: "latest"

# Domain configuration
domains:
  - domain: "my-app.com"
    aliases:
      - "www.my-app.com"

# Email for Let's Encrypt registration
acme_email: "you@email.com"
```

> [!NOTE]
> The configuration file doesn't have to live in your project directory and you can name it whatever you like, but if you don't use haloy.yaml you have to specify the path to the file. For example `haloy deploy my-app.yaml`. 

For all available options, see the full [Configuration Options](#configuration-options) table below.

### 4. Deploy

```bash
haloy deploy
```

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
| `server` | string | No | Haloy manager API URL |
| `domains` | array | No | Domain configuration |
| `acme_email` | string | No | Let's Encrypt email (required with domains) |
| `replicas` | integer | No | Number of container instances (default: 1) |
| `port` | string | No | Container port (default: "8080") |
| `health_check_path` | string | No | Health check endpoint (default: "/") |
| `env` | array | No | Environment variables |
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

## Commands

```bash
# Deploy application
haloy deploy [config-file]

# Check status
haloy status [config-file]

# List deployments
haloy rollback <app-name>

# Rollback to specific deployment
haloy rollback <app-name> <deployment-id>

# Manage secrets
haloy secrets init
haloy secrets set <name> <value>
haloy secrets list
haloy secrets delete <name>
```

### Build Locally With the `pre_deploy` hook.
To get up and running quickly with your app you can build the images locally on your own system and upload with scp to your server. Make sure to set the right platform flag for the server you are using and upload the finished image to the server. 

Here's a simple configuration illustrating how we can build and deploy without needing a Docker registry.

Note that we need to add source: local to the image configuration to indicate that we don't need to pull from a registry.
```json
  {
  "server": "https://haloy.yourserver.com",
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
## Secrets Management

```bash
# Initialize secrets system
haloy secrets init

# Store a secret
haloy secrets set api-key "your-secret-value"

# List secrets
haloy secrets list

# Use in configuration
env:
  - name: "API_KEY"
    secret_name: "api-key"
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
├── manager.yaml         # Manager settings
├── .env                 # API tokens
└── docker-compose.yml   # Service definitions

/var/lib/haloy/          # Data
├── haproxy-config/      # HAProxy configs
├── cert-storage/        # SSL certificates
└── data/                # Database files
```

**User Installation (`--local-install`):**
```
~/.config/haloy/         # Configuration
~/.local/share/haloy/    # Data
```

## Architecture

Haloy consists of several components:

1. **Haloy CLI (`haloy`)** - Command-line interface for deployments
2. **Haloy Manager** - Service discovery and configuration management
3. **HAProxy** - Load balancer and SSL termination
4. **Application Containers** - Your deployed applications

The system uses Docker labels for service discovery and dynamic configuration generation.

## Development

### Building
```bash
go build -o haloy ./cmd/cli
```

### Releasing
Create an annotated tag to trigger automated release:
```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

## License

[MIT License](LICENSE)
