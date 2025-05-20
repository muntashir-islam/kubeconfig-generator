# kubeconfig-generator

A Go utility to generate kubeconfig files for Kubernetes ServiceAccounts.

![Kubernetes](https://img.shields.io/badge/kubernetes-%23326ce5.svg?style=for-the-badge&logo=kubernetes&logoColor=white)
![GOLANG](https://img.shields.io/badge/go-%2300ADD8.svg?style=for-the-badge&logo=go&logoColor=white)
## Overview

`kubeconfig-generator` allows you to easily create kubeconfig files for ServiceAccounts in your Kubernetes cluster. This enables applications or users to authenticate to Kubernetes using a ServiceAccount identity and its associated RBAC permissions.

The tool works with both modern Kubernetes clusters (1.24+) that use the TokenRequest API and older clusters that use ServiceAccount secrets for tokens.

## Features

- Generate kubeconfig files for any existing ServiceAccount
- Works with both modern and legacy Kubernetes authentication mechanisms
- Automatically extracts cluster information from your current context
- Preserves CA certificate data for secure communication
- Supports configurable token expiration
- Handles proper file permissions for secure kubeconfig files

## Installation

### Prerequisites

- Go 1.23 or higher
- Access to a Kubernetes cluster
- `kubectl` configured with access to your cluster

### Building from source

1. Clone the repository or create the source file:

```bash
# If using git
git clone https://github.com/muntashir-islam/kubeconfig-generator.git
cd kubeconfig-generator

# Or if starting with just the main.go file
mkdir -p kubeconfig-generator
cd kubeconfig-generator
# Copy the main.go file here
```

2. Initialize the Go module and get dependencies:

```bash
go mod init kubeconfig-generator
go get k8s.io/client-go@latest
go get k8s.io/apimachinery@latest
```

3. Build the binary:

```bash
go build -o kubeconfig-generator .
```

4. (Optional) Move the binary to your PATH:

```bash
sudo mv kubeconfig-generator /usr/local/bin/
```

## Usage

### Basic usage

```bash
./kubeconfig-generator -sa SERVICE_ACCOUNT_NAME -namespace NAMESPACE -output KUBECONFIG_PATH
```

### All available options

```bash
Usage:
  kubeconfig-generator [flags]

Flags:
  -sa string            Name of the ServiceAccount (required)
  -namespace string     Namespace of the ServiceAccount (default "default")
  -output string        Output path for the kubeconfig file (default "./sa-kubeconfig")
  -context string       Context name to use in kubeconfig (defaults to <sa-name>-context)
  -cluster string       Cluster name to use in kubeconfig (defaults from current context)
  -api-server string    API server URL (defaults from current context)
  -kubeconfig string    Path to the kubeconfig file (default "~/.kube/config")
  -expiry int           Token expiry in hours (default 8760 - 1 year)
```

## Example: Creating a ServiceAccount for Pod Viewing

This example demonstrates creating a ServiceAccount with permissions to list pods in the default namespace, then generating a kubeconfig for it.

1. Create the ServiceAccount and RBAC resources:

```bash
# Create a namespace for our ServiceAccount
kubectl create namespace sa-namespace

# Create the ServiceAccount
kubectl create serviceaccount pod-viewer --namespace sa-namespace

# Create a Role with permissions to list pods in default namespace
cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: default
  name: pod-viewer-role
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "watch", "list"]
EOF

# Create a RoleBinding
cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: pod-viewer-rolebinding
  namespace: default
subjects:
- kind: ServiceAccount
  name: pod-viewer
  namespace: sa-namespace
roleRef:
  kind: Role
  name: pod-viewer-role
  apiGroup: rbac.authorization.k8s.io
EOF
```

2. Generate a kubeconfig for the ServiceAccount:

```bash
./kubeconfig-generator -sa pod-viewer -namespace sa-namespace -output ./pod-viewer-kubeconfig
```

3. Test the kubeconfig:

```bash
# Should succeed - list pods in default namespace
KUBECONFIG=./pod-viewer-kubeconfig kubectl get pods -n default

# Should fail - no permission to list pods in other namespaces
KUBECONFIG=./pod-viewer-kubeconfig kubectl get pods -n kube-system

# Should fail - no permission to delete pods
KUBECONFIG=./pod-viewer-kubeconfig kubectl delete pod some-pod-name -n default
```

## How It Works

1. The tool first loads your current kubeconfig to get cluster information (API server URL, CA certificate).
2. It verifies that the ServiceAccount exists in the specified namespace.
3. For Kubernetes 1.24+, it attempts to create a token using the `kubectl create token` command.
4. For older Kubernetes versions, it falls back to retrieving the token from the ServiceAccount's secret.
5. It constructs a new kubeconfig file with the cluster information, token, and appropriate context.
6. The file permissions are set to 0600 (read/write for owner only) for security.

## Security Considerations

- The generated kubeconfig contains a token with the permissions of the ServiceAccount
- By default, tokens are generated with a 1-year expiry (configurable with `-expiry`)
- The kubeconfig file permissions are set to be readable only by the owner
- For production use, consider setting shorter expiry times and securely distributing the kubeconfig

## Troubleshooting

### Common Issues

1. **"Failed to get ServiceAccount"**
    - Verify the ServiceAccount exists in the specified namespace
    - Check that your current kubeconfig has permissions to read ServiceAccounts

2. **"Error generating token"**
    - For older clusters: verify the ServiceAccount has an associated secret
    - For newer clusters: check that you have permissions to create tokens

3. **Permission denied with generated kubeconfig**
    - Verify the ServiceAccount has appropriate RBAC permissions
    - Check that the token is valid and has not expired

## License

MIT

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
