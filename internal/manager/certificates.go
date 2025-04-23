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
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ameistad/haloy/internal/helpers"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"github.com/rs/zerolog"
)

const (
	// Define a key for the certificate refresh debounce action
	refreshDebounceKey = "certificate_refresh"
	// Define the debounce delay for certificate refreshes
	refreshDebounceDelay = 5 * time.Second
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
	keyDir := filepath.Join(certDir, "accounts")
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
	domains       map[string]CertificatesDomain
	domainMutex   sync.RWMutex
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
		domains:       make(map[string]CertificatesDomain),
		ctx:           ctx,
		cancel:        cancel,
		clientManager: clientManager,
		updateSignal:  updateSignal, // Store the channel
		debouncer:     helpers.NewDebouncer(refreshDebounceDelay),
	}

	return m, nil
}

func (m *CertificatesManager) Start(logger zerolog.Logger) {
	// Initial check might still be direct if desired on immediate startup
	// go m.checkRenewals() // Or use Refresh() if debounce on startup is ok
	go m.renewalLoop(logger)
	go m.cleanupLoop(logger)
}

func (m *CertificatesManager) Stop() {
	m.cancel()
	m.debouncer.Stop() // Stop the debouncer to clean up any pending timers
}

// AddDomains updates the set of domains managed by the certificate manager.
// It adds new domains, updates existing ones if aliases or email change,
// and removes domains that are no longer present in the input list.
func (m *CertificatesManager) AddDomains(managedDomains []CertificatesDomain, logger zerolog.Logger) {
	m.domainMutex.Lock()
	defer m.domainMutex.Unlock()

	added := 0
	updated := 0
	removed := 0
	currentManaged := make(map[string]struct{}, len(managedDomains)) // Track for removal check

	for _, md := range managedDomains {
		if md.Canonical == "" {
			continue
		} // Skip invalid entries (no canonical domain)
		currentManaged[md.Canonical] = struct{}{} // Mark as required

		existing, exists := m.domains[md.Canonical]
		if !exists {
			// Ensure Aliases slice is initialized even if empty
			if md.Aliases == nil {
				md.Aliases = []string{}
			}
			m.domains[md.Canonical] = md // Add new domain
			added++
		} else {
			// Ensure new Aliases slice is initialized if needed
			if md.Aliases == nil {
				md.Aliases = []string{}
			}
			// Check if update needed (compare email and sorted aliases)
			sort.Strings(existing.Aliases)
			sort.Strings(md.Aliases)
			if existing.Email != md.Email || !reflect.DeepEqual(existing.Aliases, md.Aliases) {
				logger.Debug().
					Str("domain", md.Canonical).
					Msg("Updating managed domain info for (Email or Aliases changed)")
				m.domains[md.Canonical] = md // Update entry
				updated++
			}
			// If no change, do nothing
		}
	}

	// Remove domains from m.domains that are no longer in the input list
	for domain := range m.domains {
		if _, ok := currentManaged[domain]; !ok {
			delete(m.domains, domain)
			logger.Debug().
				Str("domain", domain).
				Msg("Domain is no longer managed, removing.")
			removed++
		}
	}

	// Log summary of changes
	if added > 0 || updated > 0 || removed > 0 {
		logger.Debug().
			Int("added", added).
			Int("updated", updated).
			Int("removed", removed).
			Msg("Certificate domains updated")

		// Trigger a refresh check - Refresh itself is non-blocking
		m.Refresh(logger)
	} else {
		logger.Trace().
			Msg("AddDomains called, but no changes detected in managed domains.")
	}
}

func (m *CertificatesManager) Refresh(logger zerolog.Logger) {
	logger.Debug().Msg("Refresh requested for certificate manager, using debouncer.")

	// Define the action to perform after the debounce delay
	refreshAction := func() {
		actionLogger := logger.With().Str("trigger", "debounced_refresh").Logger()
		actionLogger.Debug().Msg("Debounced refresh triggered: running checkRenewals")
		m.checkRenewals(actionLogger) // Call the actual check function
	}

	// Use the generic debouncer with a specific key for certificate refreshes
	m.debouncer.Debounce(refreshDebounceKey, refreshAction)
}

// renewalLoop periodically checks for certificates that need renewal
func (m *CertificatesManager) renewalLoop(logger zerolog.Logger) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Do an initial check after a short delay to allow startup/discovery
	time.Sleep(30 * time.Second) // Delay initial check slightly
	m.checkRenewals(logger)

	for {
		select {
		case <-ticker.C:
			logger.Debug().
				Msg("Running periodic renewal check")
			m.checkRenewals(logger)
		case <-m.ctx.Done():
			logger.Debug().
				Msg("Renewal loop stopping due to context cancellation.")
			return
		}
	}
}

// cleanupLoop periodically checks for and removes expired certificates
func (m *CertificatesManager) cleanupLoop(logger zerolog.Logger) {
	ticker := time.NewTicker(24 * time.Hour) // Check once a day
	defer ticker.Stop()

	time.Sleep(60 * time.Second) // Delay initial check slightly
	m.cleanupExpiredCertificates(logger)

	for {
		select {
		case <-ticker.C:
			logger.Debug().
				Msg("Running periodic certificate cleanup check.")
			m.cleanupExpiredCertificates(logger)
		case <-m.ctx.Done():
			logger.Debug().
				Msg("Cleanup loop stopping due to context cancellation.")
			return
		}
	}
}

func (m *CertificatesManager) checkRenewals(logger zerolog.Logger) {
	m.checkMutex.Lock()
	logger.Debug().
		Msg("Acquired renewal check lock")
	defer func() {
		logger.Debug().
			Msg("Releasing renewal check lock")
		m.checkMutex.Unlock()
	}()

	m.domainMutex.RLock()
	domainsToCheck := make(map[string]CertificatesDomain, len(m.domains))
	if len(m.domains) > 0 {
		maps.Copy(domainsToCheck, m.domains)
	}
	m.domainMutex.RUnlock()

	logger.Debug().
		Int("domains_to_check", len(domainsToCheck)).
		Msg("Checking renewals for domains")
	if len(domainsToCheck) == 0 {
		return
	}

	for domain, managedDomainInfo := range domainsToCheck {
		// File paths always use the canonical domain name (map key)
		certFilePath := filepath.Join(m.config.CertDir, domain+".crt")
		// Combined file used by HAProxy
		combinedCertKeyPath := filepath.Join(m.config.CertDir, domain+".crt.key")

		_, err := os.Stat(combinedCertKeyPath) // Check for the combined file HAProxy needs
		needsObtain := os.IsNotExist(err)
		needsRenewalDueToExpiry := false
		sanMismatch := false // Flag for SAN list mismatch

		if !needsObtain {
			// Load the .crt file to check expiry and SANs
			certData, err := os.ReadFile(certFilePath)
			if err != nil {
				logger.Error().
					Str("domain", domain).
					Err(err).
					Msg("Failed to read certificate file")
				// If we can't read the .crt file, we can't check expiry/SANs.
				// Should we attempt to obtain? Maybe, if the combined file exists but .crt is bad.
				// Let's attempt obtain if reading fails, as something is wrong.
				logger.Warn().
					Str("domain", domain).
					Msg("Marking for obtainment due to error reading .crt file.")
				needsObtain = true // Treat read error as needing obtainment
			} else {
				// Use the local parseCertificate helper
				parsedCert, err := parseCertificate(certData)
				if err != nil {
					logger.Error().
						Str("domain", domain).
						Err(err).
						Msg("Failed to parse certificate")
					// Treat parse error as needing obtainment
					logger.Warn().
						Str("domain", domain).
						Msg("Marking for obtainment due to error parsing certificate.")
					needsObtain = true
				} else {
					// Check expiry
					if time.Until(parsedCert.NotAfter) < 30*24*time.Hour {
						logger.Info().
							Str("domain", domain).
							Time("expiry", parsedCert.NotAfter).
							Msgf("Certificate for %s expires soon", domain)
						needsRenewalDueToExpiry = true
					}

					// --- Check SAN list ---
					requiredDomains := append([]string{managedDomainInfo.Canonical}, managedDomainInfo.Aliases...)
					// Ensure requiredDomains is not nil if Aliases was nil
					if requiredDomains == nil {
						requiredDomains = []string{managedDomainInfo.Canonical}
					}

					currentDomains := parsedCert.DNSNames // Get SANs from loaded cert
					if currentDomains == nil {
						currentDomains = []string{}
					} // Ensure not nil for comparison

					// Sort both slices for consistent comparison
					sort.Strings(requiredDomains)
					sort.Strings(currentDomains)

					if !reflect.DeepEqual(requiredDomains, currentDomains) {
						logger.Debug().
							Strs("required_domains", requiredDomains).
							Strs("current_domains", currentDomains).
							Msg("Required vs Current SANs")
						sanMismatch = true
					}
					// --- End SAN check ---
				}
			}
		} // end if !needsObtain (checking existing cert)

		// Trigger obtain if file doesn't exist OR expiry nearing OR SAN list mismatch
		if needsObtain || needsRenewalDueToExpiry || sanMismatch {
			logger.Debug().
				Str("domain", domain).
				Bool("obtain_needed", needsObtain).
				Bool("expiry_near", needsRenewalDueToExpiry).
				Bool("san_mismatch", sanMismatch).
				Msg("Triggering certificate obtain/renewal")
			// Pass the full info needed for the request
			m.obtainCertificate(managedDomainInfo, logger)
		} else if !os.IsNotExist(err) { // Only log skipping if we actually checked a cert file
			logger.Debug().
				Str("domain", domain).
				Msg("Skipping certificate renewal. It's valid and SANs match.")
			logger.Info().Msgf("Certificates for %s and aliases %s is valid.", domain, strings.Join(managedDomainInfo.Aliases, ", "))
		}
	} // end loop over domains
	logger.Debug().
		Msg("Finished checking renewals.")
}

// obtainCertificate requests a certificate from ACME provider for the canonical domain and its aliases.
func (m *CertificatesManager) obtainCertificate(managedDomain CertificatesDomain, logger zerolog.Logger) {
	canonicalDomain := managedDomain.Canonical
	email := managedDomain.Email
	// Ensure Aliases is not nil before appending
	aliases := managedDomain.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	allDomains := append([]string{canonicalDomain}, aliases...) // Combine canonical + aliases

	logger.Debug().
		Str("domain", canonicalDomain).
		Str("email", email).
		Strs("domains", allDomains).
		Msg("Starting certificate obtainment/renewal")

	client, err := m.clientManager.LoadOrRegisterClient(email)
	if err != nil {
		logger.Error().
			Str("domain", canonicalDomain).
			Err(err).
			Msg("Failed to load or register ACME client")
		return
	}
	logger.Debug().
		Str("domain", canonicalDomain).
		Msg("Successfully loaded ACME client")

	request := certificate.ObtainRequest{
		Domains: allDomains, // Request cert for canonical + aliases
		Bundle:  true,       // Bundle intermediate certs
	}

	logger.Debug().
		Str("domain", canonicalDomain).
		Strs("domains", allDomains).
		Msg("Requesting certificate from ACME provider")

	// INFO log.
	logger.Info().Msgf("Requesting new certificate for %s and aliases %s", canonicalDomain, strings.Join(aliases, ", "))
	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		logger.Error().
			Str("domain", canonicalDomain).
			Err(err).
			Strs("domains", allDomains).
			Msg("Failed to obtain certificate for domains")
		return
	}
	logger.Debug().
		Str("domain", canonicalDomain).
		Strs("domains", allDomains).
		Msg("Successfully obtained certificate for domains")

	logger.Debug().
		Str("domain", canonicalDomain).
		Msg("Saving certificate using canonical name")
	err = m.saveCertificate(canonicalDomain, certificates, logger)
	if err != nil {
		logger.Error().
			Str("domain", canonicalDomain).
			Err(err).
			Msg("Failed to save certificate")
		return
	}

	// INFO log.
	logger.Info().
		Str("domain", canonicalDomain).
		Msgf("Successfully fetched certificate for %s and aliases %s", canonicalDomain, strings.Join(aliases, ", "))

	// Send signal to HAProxy manager to update config
	logger.Debug().
		Str("domain", canonicalDomain).
		Msg("Signaling for HAProxy config update after obtaining certificate")
	// Send canonical domain name non-blockingly (in case channel buffer full or receiver slow)
	select {
	case m.updateSignal <- canonicalDomain:
		logger.Debug().
			Str("domain", canonicalDomain).
			Msg("Successfully signaled update")
	default:
		logger.Warn().
			Str("domain", canonicalDomain).
			Msg("Update signal channel full or closed, skipping signal")
	}

}

func (m *CertificatesManager) saveCertificate(domain string, cert *certificate.Resource, logger zerolog.Logger) error {
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

	logger.Debug().
		Str("domain", domain).
		Str("cert_path", certPath).
		Str("key_path", keyPath).
		Str("combined_path", combinedPath).
		Msg("Saved certificate files")
	return nil
}

func (m *CertificatesManager) cleanupExpiredCertificates(logger zerolog.Logger) {
	logger.Debug().
		Msg("Starting certificate cleanup check")

	files, err := os.ReadDir(m.config.CertDir)
	if err != nil {
		logger.Error().
			Err(err).
			Str("cert_dir", m.config.CertDir).
			Msg("Failed to read certificate directory")
		return
	}

	deleted := 0

	m.domainMutex.RLock()
	managedDomainsMap := make(map[string]struct{}, len(m.domains))
	for domain := range m.domains { // Keys are canonical domains
		managedDomainsMap[domain] = struct{}{}
	}
	m.domainMutex.RUnlock()

	for _, file := range files {
		// Look for the combined file HAProxy uses
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".crt.key") {
			domain := strings.TrimSuffix(file.Name(), ".crt.key")
			_, isManaged := managedDomainsMap[domain]

			// Define paths for all related files
			combinedPath := filepath.Join(m.config.CertDir, file.Name())
			certPath := filepath.Join(m.config.CertDir, domain+".crt")
			keyPath := filepath.Join(m.config.CertDir, domain+".key")

			// Check expiry using the .crt file
			certData, err := os.ReadFile(certPath)
			if err != nil {
				// If .crt is missing but .crt.key exists, log and potentially clean up if unmanaged
				if os.IsNotExist(err) && !isManaged {
					logger.Warn().
						Str("combined_path", combinedPath).
						Str("domain", domain).
						Msg("Found orphaned combined file for unmanaged domain (.crt missing). Deleting.")
					os.Remove(combinedPath)
					os.Remove(keyPath) // Try removing .key too if it exists
					deleted++
				} else if !os.IsNotExist(err) {
					// Log other read errors
					logger.Warn().
						Err(err).
						Str("cert_path", certPath).
						Msg("Failed to read certificate file during cleanup")
				}
				continue // Skip if we can't read the cert
			}

			// Use the local parseCertificate helper
			parsedCert, err := parseCertificate(certData)
			if err != nil {
				logger.Warn().
					Err(err).
					Str("cert_path", certPath).
					Msg("Failed to parse certificate during cleanup")
				continue // Skip if parsing fails
			}

			// Delete if expired AND unmanaged
			if time.Now().After(parsedCert.NotAfter) && !isManaged {
				logger.Debug().
					Str("domain", domain).
					Time("expired", parsedCert.NotAfter).
					Msg("Deleting expired certificate files for unmanaged domain")
				os.Remove(combinedPath)
				os.Remove(certPath)
				os.Remove(keyPath)
				deleted++
			}
		}
	} // end loop over files

	logger.Debug().
		Int("deleted", deleted).
		Msg("Certificate cleanup complete. Deleted expired/orphaned certificate sets for unmanaged domains")
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
	// Create key directory if it doesn't exist
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
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
