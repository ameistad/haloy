package manager

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

const (
	// Define a key for the certificate refresh debounce action
	refreshDebounceKey = "certificate_refresh"
	// Define the debounce delay for certificate refreshes
	refreshDebounceDelay = 5 * time.Second
	accountsDirName      = "accounts"
)

type CertificatesUser struct {
	Email        string
	Registration *registration.Resource
	privateKey   crypto.PrivateKey
}

func (u *CertificatesUser) GetEmail() string {
	return u.Email
}
func (u *CertificatesUser) GetRegistration() *registration.Resource {
	return u.Registration
}
func (u *CertificatesUser) GetPrivateKey() crypto.PrivateKey {
	return u.privateKey
}

type CertificatesClientManager struct {
	tlsStaging         bool
	keyManager         *CertificatesKeyManager
	clients            map[string]*lego.Client
	clientsMutex       sync.RWMutex
	sharedHTTPProvider *http01.ProviderServer
}

func NewCertificatesClientManager(certDir string, tlsStaging bool, httpProviderPort string) (*CertificatesClientManager, error) {
	keyDir := filepath.Join(certDir, accountsDirName)
	// Ensure the key directory exists
	if err := os.MkdirAll(keyDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create key directory '%s': %w", keyDir, err)
	}
	keyManager, err := NewCertificatesKeyManager(keyDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create key manager: %w", err)
	}

	httpProvider := http01.NewProviderServer("", httpProviderPort)

	return &CertificatesClientManager{
		tlsStaging:         tlsStaging,
		clients:            make(map[string]*lego.Client),
		keyManager:         keyManager,
		sharedHTTPProvider: httpProvider,
	}, nil
}

func (cm *CertificatesClientManager) LoadOrRegisterClient(email string) (*lego.Client, error) {
	// Return client early if it exists
	cm.clientsMutex.RLock()
	client, ok := cm.clients[email]
	cm.clientsMutex.RUnlock()

	if ok {
		return client, nil
	}

	// Client doesn't exist, acquire write lock for creation
	cm.clientsMutex.Lock()
	defer cm.clientsMutex.Unlock()

	// Check again in case another goroutine created it while we were waiting
	if client, ok := cm.clients[email]; ok {
		return client, nil
	}

	privateKey, err := cm.keyManager.LoadOrCreateKey(email)
	if err != nil {
		return nil, fmt.Errorf("failed to load/create user key: %w", err)
	}

	user := &CertificatesUser{
		Email:      email,
		privateKey: privateKey,
	}

	legoConfig := lego.NewConfig(user)
	if cm.tlsStaging {
		legoConfig.CADirURL = lego.LEDirectoryStaging
	} else {
		legoConfig.CADirURL = lego.LEDirectoryProduction
	}

	client, err = lego.NewClient(legoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create lego client: %w", err)
	}

	// Configure HTTP challenge provider using a server that listens on port 8080
	// HAProxy is configured to forward /.well-known/acme-challenge/* requests to this server
	err = client.Challenge.SetHTTP01Provider(cm.sharedHTTPProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to set HTTP challenge provider: %w", err)
	}

	// Register the user with the ACME server
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("failed to register user: %w", err)
	}
	user.Registration = reg

	cm.clients[email] = client

	return client, nil
}

type CertificatesManagerConfig struct {
	CertDir          string
	HTTPProviderPort string
	TlsStaging       bool
}

type CertificatesDomain struct {
	Canonical string
	Aliases   []string
	Email     string
}

type CertificatesManager struct {
	config        CertificatesManagerConfig
	checkMutex    sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	clientManager *CertificatesClientManager
	updateSignal  chan<- string // Channel to signal successful updates
	debouncer     *helpers.Debouncer
}

func NewCertificatesManager(config CertificatesManagerConfig, updateSignal chan<- string) (*CertificatesManager, error) {
	// Create directories if they don't exist
	if err := os.MkdirAll(config.CertDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	clientManager, err := NewCertificatesClientManager(config.CertDir, config.TlsStaging, config.HTTPProviderPort)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create client manager: %w", err)
	}

	m := &CertificatesManager{
		config:        config,
		ctx:           ctx,
		cancel:        cancel,
		clientManager: clientManager,
		updateSignal:  updateSignal, // Store the channel
		debouncer:     helpers.NewDebouncer(refreshDebounceDelay),
	}

	return m, nil
}

func (m *CertificatesManager) Stop() {
	m.cancel()
	m.debouncer.Stop() // Stop the debouncer to clean up any pending timers
}

// RefereshSync is used for synchronous refreshes of certificates for app updates.
func (cm *CertificatesManager) RefreshSync(logger *logging.Logger, domains []CertificatesDomain) {
	renewedDomains, err := cm.checkRenewals(logger, domains)
	if err != nil {
		logger.Error("Certificate refresh failed", err)
	}
	if len(renewedDomains) > 0 {
		for _, domain := range renewedDomains {
			logger.Info(fmt.Sprintf("CertificatesManager: Renewed certificate for %s and aliases %s", domain.Canonical, strings.Join(domain.Aliases, ", ")))
		}
	}
}

// Refresh is used for periodoc refreshes of certificates.
func (cm *CertificatesManager) Refresh(logger *logging.Logger, domains []CertificatesDomain) {
	logger.Debug("Refresh requested for certificate manager, using debouncer.")

	// Define the action to perform after the debounce delay
	refreshAction := func() {
		renewedDomains, err := cm.checkRenewals(logger, domains)
		if err != nil {
			logger.Error("Certificate refresh failed", err)
			return
		}
		// Signal the update channel to update HAProxy on async Refresh
		// Only signal if we actually renewed something
		if len(renewedDomains) > 0 {
			for _, domain := range renewedDomains {
				logger.Info(fmt.Sprintf("CertificatesManager: Renewed certificate for %s and aliases %s", domain.Canonical, strings.Join(domain.Aliases, ", ")))
			}
			if cm.updateSignal != nil {
				cm.updateSignal <- "certificates_renewed"
			}
		} else {
			logger.Debug("CertificatesManager: No certificates needed renewal at this time.")
		}
	}

	// Use the generic debouncer with a specific key for certificate refreshes
	cm.debouncer.Debounce(refreshDebounceKey, refreshAction)
}

func (cm *CertificatesManager) checkRenewals(logger *logging.Logger, domains []CertificatesDomain) (renewedDomains []CertificatesDomain, err error) {
	cm.checkMutex.Lock()
	defer func() {
		cm.checkMutex.Unlock()
	}()

	if len(domains) == 0 {
		return renewedDomains, nil
	}

	// Debug: Log all domains being processed
	logger.Debug(fmt.Sprintf("Processing %d domains for certificate renewal check", len(domains)))
	for i, domain := range domains {
		logger.Debug(fmt.Sprintf("Domain %d: %s with aliases %v (email: %s)", i, domain.Canonical, domain.Aliases, domain.Email))
	}

	// Build the current desired state - only one entry per canonical domain
	currentState := make(map[string]CertificatesDomain)
	for _, domain := range domains {
		if existing, exists := currentState[domain.Canonical]; exists {
			// Prefer the configuration with more aliases
			if len(domain.Aliases) > len(existing.Aliases) {
				logger.Info(fmt.Sprintf("Using domain %s configuration with more aliases: %v (over %v)", domain.Canonical, domain.Aliases, existing.Aliases))
				currentState[domain.Canonical] = domain
			} else {
				logger.Debug(fmt.Sprintf("Keeping existing domain %s configuration with aliases: %v", domain.Canonical, existing.Aliases))
			}
		} else {
			currentState[domain.Canonical] = domain
		}
	}

	// Process each domain in the current desired state
	for canonical, domain := range currentState {
		// Check if this domain configuration has changed
		configChanged, err := cm.hasConfigurationChanged(logger, domain)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to check configuration for %s: %v", canonical, err))
			continue
		}

		// Check if certificate needs renewal due to expiry
		needsRenewal, err := cm.needsRenewalDueToExpiry(logger, domain)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to check expiry for %s: %v", canonical, err))
			// Treat error as needing renewal to be safe
			needsRenewal = true
		}

		// If configuration changed, clean up all related certificates first
		if configChanged {
			logger.Info(fmt.Sprintf("Configuration changed for %s, cleaning up existing certificates", canonical))
			if err := cm.cleanupDomainCertificates(logger, canonical); err != nil {
				logger.Warn(fmt.Sprintf("Failed to cleanup certificates for %s: %v", canonical, err))
				// Continue anyway, might still work
			}
		}

		// Obtain certificate if needed
		if configChanged || needsRenewal {
			obtainedDomain, err := cm.obtainCertificate(domain, logger)
			if err != nil {
				logger.Error(fmt.Sprintf("Failed to obtain certificate for %s: %v", canonical, err))
			} else {
				renewedDomains = append(renewedDomains, obtainedDomain)
			}
		} else {
			var aliasMsg string
			if len(domain.Aliases) > 0 {
				aliasMsg = fmt.Sprintf(" (aliases: %s)", strings.Join(domain.Aliases, ", "))
			}
			logger.Info(fmt.Sprintf("Skipping renewal for %s%s: certificate is valid and configuration unchanged.", canonical, aliasMsg))
		}
	}

	return renewedDomains, nil
}

// hasConfigurationChanged checks if the domain configuration has changed compared to existing certificate
func (cm *CertificatesManager) hasConfigurationChanged(logger *logging.Logger, domain CertificatesDomain) (bool, error) {
	certFilePath := filepath.Join(cm.config.CertDir, domain.Canonical+".crt")
	combinedCertKeyPath := filepath.Join(cm.config.CertDir, domain.Canonical+".crt.key")

	// If certificate files don't exist, configuration has "changed" (need to create)
	if _, err := os.Stat(combinedCertKeyPath); os.IsNotExist(err) {
		logger.Debug(fmt.Sprintf("%s: Certificate files don't exist, needs creation", domain.Canonical))
		return true, nil
	}

	// Read and parse existing certificate
	certData, err := os.ReadFile(certFilePath)
	if err != nil {
		logger.Debug(fmt.Sprintf("%s: Cannot read certificate file, treating as changed", domain.Canonical))
		return true, nil
	}

	parsedCert, err := parseCertificate(certData)
	if err != nil {
		logger.Debug(fmt.Sprintf("%s: Cannot parse certificate, treating as changed", domain.Canonical))
		return true, nil
	}

	// Build required domains list from current configuration
	requiredDomains := []string{domain.Canonical}
	if len(domain.Aliases) > 0 {
		requiredDomains = append(requiredDomains, domain.Aliases...)
	}
	sort.Strings(requiredDomains)

	// Get domains from existing certificate
	existingDomains := parsedCert.DNSNames
	if existingDomains == nil {
		existingDomains = []string{}
	}
	sort.Strings(existingDomains)

	// Check if email has changed by comparing with metadata file
	metadataPath := filepath.Join(cm.config.CertDir, domain.Canonical+".meta")
	if metadataData, err := os.ReadFile(metadataPath); err == nil {
		if strings.TrimSpace(string(metadataData)) != domain.Email {
			logger.Info(fmt.Sprintf("%s: Email changed from %s to %s", domain.Canonical, strings.TrimSpace(string(metadataData)), domain.Email))
			return true, nil
		}
	}

	// Compare domain lists
	configChanged := !reflect.DeepEqual(requiredDomains, existingDomains)
	if configChanged {
		logger.Info(fmt.Sprintf("%s: Configuration changed. Required: %v, Existing: %v", domain.Canonical, requiredDomains, existingDomains))
	}

	return configChanged, nil
}

// needsRenewalDueToExpiry checks if certificate needs renewal due to expiry
func (cm *CertificatesManager) needsRenewalDueToExpiry(logger *logging.Logger, domain CertificatesDomain) (bool, error) {
	certFilePath := filepath.Join(cm.config.CertDir, domain.Canonical+".crt")

	// If certificate doesn't exist, we need to obtain one
	certData, err := os.ReadFile(certFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // File doesn't exist, need to obtain
		}
		return false, err // Other error
	}

	parsedCert, err := parseCertificate(certData)
	if err != nil {
		return true, nil // Can't parse, need to obtain new one
	}

	// Check if certificate expires within 30 days
	if time.Until(parsedCert.NotAfter) < 30*24*time.Hour {
		logger.Info(fmt.Sprintf("%s: Certificate expires soon and needs renewal", domain.Canonical))
		return true, nil
	}

	return false, nil
}

// cleanupDomainCertificates removes all certificate files for a domain
func (cm *CertificatesManager) cleanupDomainCertificates(logger *logging.Logger, canonical string) error {
	certPath := filepath.Join(cm.config.CertDir, canonical+".crt")
	keyPath := filepath.Join(cm.config.CertDir, canonical+".key")
	combinedPath := filepath.Join(cm.config.CertDir, canonical+".crt.key")
	metadataPath := filepath.Join(cm.config.CertDir, canonical+".meta")

	files := []string{certPath, keyPath, combinedPath, metadataPath}
	var errors []error

	for _, file := range files {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			errors = append(errors, fmt.Errorf("failed to remove %s: %w", file, err))
		} else if err == nil {
			logger.Debug(fmt.Sprintf("Removed %s", file))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup partially failed: %v", errors)
	}

	return nil
}



// obtainCertificate requests a certificate from ACME provider for the canonical domain and its aliases.
func (m *CertificatesManager) obtainCertificate(managedDomain CertificatesDomain, logger *logging.Logger) (obtainedDomain CertificatesDomain, err error) {
	canonicalDomain := managedDomain.Canonical
	email := managedDomain.Email
	// Ensure Aliases is not nil before appending
	aliases := managedDomain.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	allDomains := append([]string{canonicalDomain}, aliases...)

	client, err := m.clientManager.LoadOrRegisterClient(email)
	if err != nil {
		return obtainedDomain, fmt.Errorf("failed to load or register ACME client for %s: %w", email, err)
	}

	request := certificate.ObtainRequest{
		Domains: allDomains, // Request cert for canonical + aliases
		Bundle:  true,       // Bundle intermediate certs
	}

	logger.Debug(fmt.Sprintf("%s: Requesting certificate from ACME provider", canonicalDomain))

	logger.Info(fmt.Sprintf("Requesting new certificate for %s and aliases %s", canonicalDomain, strings.Join(aliases, ", ")))
	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		return obtainedDomain, fmt.Errorf("failed to obtain certificate for %s: %w", canonicalDomain, err)
	}
	err = m.saveCertificate(canonicalDomain, certificates, email, logger)
	if err != nil {
		return obtainedDomain, fmt.Errorf("failed to save certificate for %s: %w", canonicalDomain, err)
	} else {
		obtainedDomain = CertificatesDomain{
			Canonical: canonicalDomain,
			Aliases:   aliases,
			Email:     email,
		}
	}

	return obtainedDomain, nil
}

func (m *CertificatesManager) saveCertificate(domain string, cert *certificate.Resource, email string, logger *logging.Logger) error {
	// Save certificate (.crt)
	certPath := filepath.Join(m.config.CertDir, domain+".crt")
	if err := os.WriteFile(certPath, cert.Certificate, 0644); err != nil {
		return fmt.Errorf("failed to save certificate: %w", err)
	}

	// Save private key (.key)
	keyPath := filepath.Join(m.config.CertDir, domain+".key")
	if err := os.WriteFile(keyPath, cert.PrivateKey, 0600); err != nil {
		// Attempt cleanup of .crt file if key saving fails
		os.Remove(certPath)
		return fmt.Errorf("failed to save private key: %w", err)
	}

	// Create combined file for HAProxy (.crt.key) - some CAs might include key in Certificate field, others separate
	combinedPath := filepath.Join(m.config.CertDir, domain+".crt.key")
	// Ensure correct order: Cert first, then Key
	pemContent := bytes.Buffer{}
	pemContent.Write(cert.Certificate)
	// Add newline separator if cert doesn't end with one
	if len(cert.Certificate) > 0 && cert.Certificate[len(cert.Certificate)-1] != '\n' {
		pemContent.WriteByte('\n')
	}
	pemContent.Write(cert.PrivateKey)

	if err := os.WriteFile(combinedPath, pemContent.Bytes(), 0600); err != nil {
		// Attempt cleanup of .crt and .key files if combined saving fails
		os.Remove(certPath)
		os.Remove(keyPath)
		return fmt.Errorf("failed to save combined certificate/key: %w", err)
	}

	// Save metadata (email) for configuration change detection
	metadataPath := filepath.Join(m.config.CertDir, domain+".meta")
	if err := os.WriteFile(metadataPath, []byte(email), 0644); err != nil {
		// Cleanup on metadata save failure
		os.Remove(certPath)
		os.Remove(keyPath)
		os.Remove(combinedPath)
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	logger.Debug(fmt.Sprintf("%s: Saved certificate files", domain))
	return nil
}

func (m *CertificatesManager) CleanupExpiredCertificates(logger *logging.Logger, domains []CertificatesDomain) {
	logger.Debug("Starting certificate cleanup check")

	files, err := os.ReadDir(m.config.CertDir)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to read certificates directory: %s", m.config.CertDir), err)
		return
	}

	deleted := 0

	managedDomainsMap := make(map[string]struct{}, len(domains))
	for _, domain := range domains { // Keys are canonical domains
		managedDomainsMap[domain.Canonical] = struct{}{}
	}

	for _, file := range files {
		// Look for the combined file HAProxy uses
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".crt.key") {
			domain := strings.TrimSuffix(file.Name(), ".crt.key")
			_, isManaged := managedDomainsMap[domain]

			// Define paths for all related files
			combinedPath := filepath.Join(m.config.CertDir, file.Name())
			certPath := filepath.Join(m.config.CertDir, domain+".crt")
			keyPath := filepath.Join(m.config.CertDir, domain+".key")
			metadataPath := filepath.Join(m.config.CertDir, domain+".meta")

			// Check expiry using the .crt file
			certData, err := os.ReadFile(certPath)
			if err != nil {
				// If .crt is missing but .crt.key exists, log and potentially clean up if unmanaged
				if os.IsNotExist(err) && !isManaged {
					logger.Warn(fmt.Sprintf("%s: Found orphaned combined file for unmanaged domain (.crt missing). Deleting.", domain))
					os.Remove(combinedPath)
					os.Remove(keyPath) // Try removing .key too if it exists
					os.Remove(metadataPath) // Try removing .meta too if it exists
					deleted++
				} else if !os.IsNotExist(err) {
					// Log other read errors
					logger.Warn(fmt.Sprintf("Failed to read certificate file during cleanup: %s", certPath), err)
				}
				continue // Skip if we can't read the cert
			}

			// Use the local parseCertificate helper
			parsedCert, err := parseCertificate(certData)
			if err != nil {
				logger.Warn(fmt.Sprintf("Failed to parse certificate during cleanup: %s", certPath))
				continue // Skip if parsing fails
			}

			// Delete if expired AND unmanaged
			if time.Now().After(parsedCert.NotAfter) && !isManaged {
				logger.Debug(fmt.Sprintf("%s: Deleting expired certificate files for unmanaged domain", domain))
				os.Remove(combinedPath)
				os.Remove(certPath)
				os.Remove(keyPath)
				os.Remove(metadataPath)
				deleted++
			}
		}
	}

	logger.Debug("Certificate cleanup complete. Deleted expired/orphaned certificate sets for unmanaged domains")
}

// parseCertificate takes PEM encoded certificate data and returns the parsed x509.Certificate
// Kept unexported as it's only used internally by checkRenewals and cleanup.
func parseCertificate(certData []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certData)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("failed to decode PEM block containing certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}
	return cert, nil
}

// CertificatesKeyManager handles private key operations for the ACME client
type CertificatesKeyManager struct {
	keyDir string
}

// NewCertificatesKeyManager creates a new key manager
func NewCertificatesKeyManager(keyDir string) (*CertificatesKeyManager, error) {
	stat, err := os.Stat(keyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("key directory '%s' does not exist; ensure init process has created it", keyDir)
		}
		return nil, fmt.Errorf("failed to stat key directory '%s': %w", keyDir, err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("key directory path '%s' is not a directory", keyDir)
	}

	return &CertificatesKeyManager{
		keyDir: keyDir,
	}, nil
}

// LoadOrCreateKey loads an existing account key or creates a new one
func (km *CertificatesKeyManager) LoadOrCreateKey(email string) (crypto.PrivateKey, error) {
	// Sanitize email for filename
	filename := helpers.SanitizeFilename(email) + ".key"
	keyPath := filepath.Join(km.keyDir, filename)

	// Check if key already exists
	if _, err := os.Stat(keyPath); err == nil {
		// Key exists, load it
		return km.loadKey(keyPath)
	}

	// Key doesn't exist, create a new one
	return km.createKey(keyPath)
}

// loadKey loads a private key from disk
func (km *CertificatesKeyManager) loadKey(path string) (crypto.PrivateKey, error) {
	// Read key file
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	// Decode PEM
	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	// Parse private key
	switch keyBlock.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(keyBlock.Bytes)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", keyBlock.Type)
	}
}

// createKey creates a new ECDSA private key and saves it to disk
func (km *CertificatesKeyManager) createKey(path string) (crypto.PrivateKey, error) {
	// Generate new ECDSA key (P-256 for good balance of security and performance)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Encode private key to PEM
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Create PEM block
	pemBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}

	// Write key to file
	keyFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	if err := pem.Encode(keyFile, pemBlock); err != nil {
		return nil, fmt.Errorf("failed to write key file: %w", err)
	}

	return privateKey, nil
}
