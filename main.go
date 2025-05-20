package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/homedir"
)

// Config holds the configuration for the kubeconfig generator
type Config struct {
	ServiceAccountName string
	Namespace          string
	OutputPath         string
	ContextName        string
	ClusterName        string
	APIServer          string
	KubeconfigPath     string
	TokenExpiryHours   int
}

func main() {
	var config Config

	// Define command-line flags
	flag.StringVar(&config.ServiceAccountName, "sa", "", "Name of the ServiceAccount (required)")
	flag.StringVar(&config.Namespace, "namespace", "default", "Namespace of the ServiceAccount")
	flag.StringVar(&config.OutputPath, "output", "sa-kubeconfig", "Output path for the kubeconfig file")
	flag.StringVar(&config.ContextName, "context", "", "Context name to use in kubeconfig (defaults to <sa-name>-context)")
	flag.StringVar(&config.ClusterName, "cluster", "", "Cluster name to use in kubeconfig (defaults from current context)")
	flag.StringVar(&config.APIServer, "api-server", "", "API server URL (defaults from current context)")
	flag.StringVar(&config.KubeconfigPath, "kubeconfig", defaultKubeconfigPath(), "Path to the kubeconfig file")
	flag.IntVar(&config.TokenExpiryHours, "expiry", 8760, "Token expiry in hours (default 1 year)")

	flag.Parse()

	// Validate required flags
	if config.ServiceAccountName == "" {
		log.Fatal("Error: ServiceAccount name is required")
	}

	// Set default context name if not provided
	if config.ContextName == "" {
		config.ContextName = fmt.Sprintf("%s-context", config.ServiceAccountName)
	}

	// Generate kubeconfig
	if err := generateKubeconfig(config); err != nil {
		log.Fatalf("Error generating kubeconfig: %v", err)
	}

	fmt.Printf("Kubeconfig file created at: %s\n", config.OutputPath)
	fmt.Printf("Use with: export KUBECONFIG=%s\n", config.OutputPath)
}

func defaultKubeconfigPath() string {
	if home := homedir.HomeDir(); home != "" {
		return filepath.Join(home, ".kube", "config")
	}
	return ""
}

func generateKubeconfig(config Config) error {
	// Load the kubeconfig file
	currentConfig, err := clientcmd.LoadFromFile(config.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Create Kubernetes clientset
	clientConfig, err := clientcmd.BuildConfigFromFlags("", config.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to build config from flags: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Get current context and cluster info
	currentContext := currentConfig.Contexts[currentConfig.CurrentContext]
	if currentContext == nil {
		return fmt.Errorf("no current context found")
	}

	currentCluster := currentConfig.Clusters[currentContext.Cluster]
	if currentCluster == nil {
		return fmt.Errorf("no cluster found for current context")
	}

	// Set default cluster name if not provided
	if config.ClusterName == "" {
		config.ClusterName = currentContext.Cluster
	}

	// Set default API server if not provided
	if config.APIServer == "" {
		config.APIServer = currentCluster.Server
	}

	// Verify the ServiceAccount exists
	_, err = clientset.CoreV1().ServiceAccounts(config.Namespace).Get(
		context.TODO(),
		config.ServiceAccountName,
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to get ServiceAccount %s in namespace %s: %w",
			config.ServiceAccountName, config.Namespace, err)
	}

	// Get service account token
	token, err := getServiceAccountToken(clientset, config)
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	// Create a new kubeconfig
	newConfig := api.NewConfig()

	// Add cluster
	newConfig.Clusters[config.ClusterName] = &api.Cluster{
		Server: config.APIServer,
	}

	// Add CA certificate data if available
	if len(currentCluster.CertificateAuthorityData) > 0 {
		newConfig.Clusters[config.ClusterName].CertificateAuthorityData = currentCluster.CertificateAuthorityData
	} else if currentCluster.CertificateAuthority != "" {
		caData, err := os.ReadFile(currentCluster.CertificateAuthority)
		if err == nil {
			newConfig.Clusters[config.ClusterName].CertificateAuthorityData = caData
		} else {
			fmt.Printf("Warning: Failed to read CA certificate: %v\n", err)
			fmt.Println("Setting insecure-skip-tls-verify: true")
			newConfig.Clusters[config.ClusterName].InsecureSkipTLSVerify = true
		}
	} else {
		fmt.Println("Warning: No CA certificate data found. Setting insecure-skip-tls-verify: true")
		newConfig.Clusters[config.ClusterName].InsecureSkipTLSVerify = true
	}

	// Add user with token
	newConfig.AuthInfos[config.ServiceAccountName] = &api.AuthInfo{
		Token: token,
	}

	// Add context
	newConfig.Contexts[config.ContextName] = &api.Context{
		Cluster:   config.ClusterName,
		AuthInfo:  config.ServiceAccountName,
		Namespace: config.Namespace,
	}

	// Set current context
	newConfig.CurrentContext = config.ContextName

	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(config.OutputPath)
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Write the kubeconfig to file
	if err := clientcmd.WriteToFile(*newConfig, config.OutputPath); err != nil {
		return fmt.Errorf("failed to write kubeconfig to file: %w", err)
	}

	// Set file permissions to 0600 (rw-------)
	if err := os.Chmod(config.OutputPath, 0600); err != nil {
		return fmt.Errorf("failed to set kubeconfig file permissions: %w", err)
	}

	return nil
}

// getServiceAccountToken gets a token for the service account using direct API call
func getServiceAccountToken(clientset *kubernetes.Clientset, config Config) (string, error) {
	// First, try to use kubectl to create a token (for newer Kubernetes versions)
	if token, err := createTokenWithKubectl(config); err == nil && token != "" {
		return token, nil
	}

	// Fall back to getting a token from a secret (for older Kubernetes versions)
	return getTokenFromSecret(clientset, config)
}

// createTokenWithKubectl tries to create a token using kubectl command
func createTokenWithKubectl(config Config) (string, error) {
	// Try using kubectl create token
	kubeconfigFlag := ""
	if config.KubeconfigPath != "" {
		kubeconfigFlag = fmt.Sprintf("--kubeconfig=%s", config.KubeconfigPath)
	}

	// Build command arguments
	args := []string{"create", "token", config.ServiceAccountName, "-n", config.Namespace}
	if kubeconfigFlag != "" {
		args = append(args, kubeconfigFlag)
	}
	args = append(args, fmt.Sprintf("--duration=%dh", config.TokenExpiryHours))

	// Execute the command and capture output
	cmd := exec.Command("kubectl", args...)
	out, err := cmd.Output()
	if err != nil {
		// This is expected to fail on older Kubernetes versions
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}

// getTokenFromSecret gets a token from the service account's secret
func getTokenFromSecret(clientset *kubernetes.Clientset, config Config) (string, error) {
	// Get ServiceAccount to find its secrets
	sa, err := clientset.CoreV1().ServiceAccounts(config.Namespace).Get(
		context.TODO(),
		config.ServiceAccountName,
		metav1.GetOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to get ServiceAccount: %w", err)
	}

	// Check if the ServiceAccount has any secrets
	if len(sa.Secrets) == 0 {
		return "", fmt.Errorf("service account has no secrets")
	}

	// Get the first secret (token secret)
	secretName := sa.Secrets[0].Name
	secret, err := clientset.CoreV1().Secrets(config.Namespace).Get(
		context.TODO(),
		secretName,
		metav1.GetOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	// Get token from secret
	tokenData, ok := secret.Data["token"]
	if !ok {
		return "", fmt.Errorf("token not found in secret %s", secretName)
	}

	return string(tokenData), nil
}
