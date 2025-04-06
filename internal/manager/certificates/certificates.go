package certificates

import (
	"bytes"
	"context"
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

	"github.com/go-acme/lego/v4/certificate"
	"github.com/sirupsen/logrus"
)

type Config struct {
	CertDir          string
	HTTPProviderPort string
	Logger           *logrus.Logger
	TlsStaging       bool
}

type ManagedDomain struct {
	Canonical string
	Aliases   []string
	Email     string
}

type Manager struct {
	config        Config
	logger        *logrus.Logger
	domains       map[string]ManagedDomain
	domainMutex   sync.RWMutex
	refreshMutex  sync.Mutex
	refreshTimer  *time.Timer
	refreshNeeded bool
	checkMutex    sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	clientManager *ClientManager
	updateSignal  chan<- string // Channel to signal successful updates
}

func NewManager(config Config, updateSignal chan<- string) (*Manager, error) {
	if config.Logger == nil {
		config.Logger = logrus.New()
	}

	// Create directories if they don't exist
	if err := os.MkdirAll(config.CertDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	clientManager, err := NewClientManager(config.CertDir, config.TlsStaging, config.HTTPProviderPort)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create client manager: %w", err)
	}

	m := &Manager{
		config:        config,
		logger:        config.Logger,
		domains:       make(map[string]ManagedDomain),
		ctx:           ctx,
		cancel:        cancel,
		clientManager: clientManager,
		updateSignal:  updateSignal, // Store the channel
	}

	return m, nil
}

func (m *Manager) Start() {
	// Initial check might still be direct if desired on immediate startup
	// go m.checkRenewals() // Or use Refresh() if debounce on startup is ok
	go m.renewalLoop()
	go m.cleanupLoop()
}

func (m *Manager) Stop() {
	m.cancel()

	m.refreshMutex.Lock()
	defer m.refreshMutex.Unlock()
	if m.refreshTimer != nil {
		m.refreshTimer.Stop()
		m.refreshTimer = nil
	}
}

// AddDomains updates the set of domains managed by the certificate manager.
// It adds new domains, updates existing ones if aliases or email change,
// and removes domains that are no longer present in the input list.
func (m *Manager) AddDomains(managedDomains []ManagedDomain) {
	m.domainMutex.Lock()
	defer m.domainMutex.Unlock()

	added := 0
	updated := 0
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
				m.logger.Infof("Updating managed domain info for %s (Email or Aliases changed)", md.Canonical)
				m.domains[md.Canonical] = md // Update entry
				updated++
			}
			// If no change, do nothing
		}
	}

	// Remove domains from m.domains that are no longer in the input list
	removed := 0
	for domain := range m.domains {
		if _, ok := currentManaged[domain]; !ok {
			delete(m.domains, domain)
			m.logger.Infof("Domain %s is no longer managed, removing.", domain)
			removed++
		}
	}

	// Log summary of changes
	if added > 0 || updated > 0 || removed > 0 {
		m.logger.Infof("Certificate domains updated: %d added, %d updated, %d removed.", added, updated, removed)
		// Trigger a refresh check if changes occurred
		go m.Refresh() // Run in background to avoid blocking caller
	} else {
		m.logger.Debug("AddDomains called, but no changes detected in managed domains.")
	}
}

func (m *Manager) Refresh() {
	m.logger.Infof("Refresh requested for certificate manager")
	m.refreshMutex.Lock()
	defer m.refreshMutex.Unlock()
	m.refreshNeeded = true

	// If a timer is already running, reset it
	if m.refreshTimer != nil {
		m.logger.Debug("Resetting existing debounce timer")
		m.refreshTimer.Stop()
	}

	// Use timer to debounce refresh requests - reduced from 15s to 5s for faster response
	m.refreshTimer = time.AfterFunc(5*time.Second, func() {
		m.refreshMutex.Lock()
		needsRun := m.refreshNeeded
		m.refreshNeeded = false // Reset flag *before* running
		m.refreshTimer = nil    // Clear the timer reference
		m.refreshMutex.Unlock()

		if needsRun {
			m.logger.Infof("Debounced refresh triggered: running checkRenewals")
			m.checkRenewals()
		} else {
			m.logger.Infof("Debounced refresh timer fired, but no longer needed.")
		}
	})
	m.logger.Infof("Refresh debouncer timer started/reset.")
}

// renewalLoop periodically checks for certificates that need renewal
func (m *Manager) renewalLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Do an initial check after a short delay to allow startup/discovery
	time.Sleep(30 * time.Second) // Delay initial check slightly
	m.checkRenewals()

	for {
		select {
		case <-ticker.C:
			m.logger.Infof("Running periodic renewal check")
			m.checkRenewals()
		case <-m.ctx.Done():
			m.logger.Info("Renewal loop stopping due to context cancellation.")
			return
		}
	}
}

// cleanupLoop periodically checks for and removes expired certificates
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour) // Check once a day
	defer ticker.Stop()

	time.Sleep(60 * time.Second) // Delay initial check slightly
	m.cleanupExpiredCertificates()

	for {
		select {
		case <-ticker.C:
			m.logger.Info("Running periodic certificate cleanup check.")
			m.cleanupExpiredCertificates()
		case <-m.ctx.Done():
			m.logger.Info("Cleanup loop stopping due to context cancellation.")
			return
		}
	}
}

func (m *Manager) checkRenewals() {
	// --- Lock to prevent concurrent checks ---
	m.checkMutex.Lock()
	m.logger.Debug("Acquired renewal check lock")
	defer func() {
		m.logger.Debug("Releasing renewal check lock")
		m.checkMutex.Unlock()
	}()
	// --- End Lock ---

	m.domainMutex.RLock()
	// Fix: Initialize domainsToCheck with the correct type
	domainsToCheck := make(map[string]ManagedDomain, len(m.domains))
	if len(m.domains) > 0 {
		maps.Copy(domainsToCheck, m.domains) // Use maps.Copy (Go 1.21+) or manual copy loop
	}
	m.domainMutex.RUnlock()

	m.logger.Infof("Checking renewals for %d domains", len(domainsToCheck))
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
				m.logger.Errorf("Failed to read certificate file %s: %v", certFilePath, err)
				// If we can't read the .crt file, we can't check expiry/SANs.
				// Should we attempt to obtain? Maybe, if the combined file exists but .crt is bad.
				// Let's attempt obtain if reading fails, as something is wrong.
				m.logger.Warnf("Marking %s for obtainment due to error reading .crt file.", domain)
				needsObtain = true // Treat read error as needing obtainment
			} else {
				// Use the local parseCertificate helper
				parsedCert, err := parseCertificate(certData)
				if err != nil {
					m.logger.Errorf("Failed to parse certificate %s: %v", certFilePath, err)
					// Treat parse error as needing obtainment
					m.logger.Warnf("Marking %s for obtainment due to error parsing certificate.", domain)
					needsObtain = true
				} else {
					// Check expiry
					if time.Until(parsedCert.NotAfter) < 30*24*time.Hour {
						m.logger.Infof("Certificate for %s expires soon (%s), marking for renewal", domain, parsedCert.NotAfter)
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
						m.logger.Infof("Certificate SAN list for %s needs update. Required: %v, Current: %v", domain, requiredDomains, currentDomains)
						sanMismatch = true
					}
					// --- End SAN check ---
				}
			}
		} // end if !needsObtain (checking existing cert)

		// Trigger obtain if file doesn't exist OR expiry nearing OR SAN list mismatch
		if needsObtain || needsRenewalDueToExpiry || sanMismatch {
			m.logger.Infof("Triggering certificate obtain/renewal for %s (ObtainNeeded: %t, Expiry: %t, SANMismatch: %t)",
				domain, needsObtain, needsRenewalDueToExpiry, sanMismatch)
			// Pass the full info needed for the request
			m.obtainCertificate(managedDomainInfo)
		} else if !os.IsNotExist(err) { // Only log skipping if we actually checked a cert file
			m.logger.Debugf("Skipping certificate renewal for %s. It's valid and SANs match.", domain)
		}
	} // end loop over domains
	m.logger.Info("Finished checking renewals.")
}

// obtainCertificate requests a certificate from ACME provider for the canonical domain and its aliases.
func (m *Manager) obtainCertificate(managedDomain ManagedDomain) {
	canonicalDomain := managedDomain.Canonical
	email := managedDomain.Email
	// Ensure Aliases is not nil before appending
	aliases := managedDomain.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	allDomains := append([]string{canonicalDomain}, aliases...) // Combine canonical + aliases

	m.logger.Infof("Starting certificate obtainment/renewal for domains: %v with email: %s", allDomains, email)

	client, err := m.clientManager.LoadOrRegisterClient(email)
	if err != nil {
		m.logger.Errorf("Failed to load or register ACME client for %s: %v", email, err)
		return
	}
	m.logger.Infof("Successfully loaded ACME client for email: %s", email)

	request := certificate.ObtainRequest{
		Domains: allDomains, // Request cert for canonical + aliases
		Bundle:  true,       // Bundle intermediate certs
	}

	m.logger.Infof("Requesting certificate from ACME provider for domains: %v", allDomains)
	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		m.logger.Errorf("Failed to obtain certificate for domains %v: %v", allDomains, err)
		return
	}
	m.logger.Infof("Successfully obtained certificate for domains: %v", allDomains)

	m.logger.Infof("Saving certificate using canonical name: %s", canonicalDomain)
	err = m.saveCertificate(canonicalDomain, certificates)
	if err != nil {
		m.logger.Errorf("Failed to save certificate %s: %v", canonicalDomain, err)
		return
	}
	m.logger.Infof("Successfully saved certificate %s", canonicalDomain)

	// --- Signal Main Loop ---
	m.logger.Infof("Signaling for HAProxy config update after obtaining certificate for %s", canonicalDomain)
	// Send canonical domain name non-blockingly (in case channel buffer full or receiver slow)
	select {
	case m.updateSignal <- canonicalDomain:
		m.logger.Debugf("Successfully signaled update for %s", canonicalDomain)
	default:
		m.logger.Warnf("Update signal channel full or closed, skipping signal for %s", canonicalDomain)
	}
	// --- End Signal Main Loop ---
}

func (m *Manager) saveCertificate(domain string, cert *certificate.Resource) error {
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

	m.logger.Debugf("Saved certificate files for %s: %s, %s, %s", domain, certPath, keyPath, combinedPath)
	return nil
}

func (m *Manager) cleanupExpiredCertificates() {
	m.logger.Info("Starting certificate cleanup check")

	files, err := os.ReadDir(m.config.CertDir)
	if err != nil {
		m.logger.Errorf("Failed to read certificate directory %s: %v", m.config.CertDir, err)
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
					m.logger.Warnf("Found orphaned combined file %s for unmanaged domain %s (.crt missing). Deleting.", combinedPath, domain)
					os.Remove(combinedPath)
					os.Remove(keyPath) // Try removing .key too if it exists
					deleted++
				} else if !os.IsNotExist(err) {
					// Log other read errors
					m.logger.Warnf("Failed to read certificate file %s during cleanup: %v", certPath, err)
				}
				continue // Skip if we can't read the cert
			}

			// Use the local parseCertificate helper
			parsedCert, err := parseCertificate(certData)
			if err != nil {
				m.logger.Warnf("Failed to parse certificate %s during cleanup: %v", certPath, err)
				continue // Skip if parsing fails
			}

			// Delete if expired AND unmanaged
			if time.Now().After(parsedCert.NotAfter) && !isManaged {
				m.logger.Infof("Deleting expired certificate files for unmanaged domain %s (Expired: %s)", domain, parsedCert.NotAfter)
				os.Remove(combinedPath)
				os.Remove(certPath)
				os.Remove(keyPath)
				deleted++
			}
		}
	} // end loop over files

	m.logger.Infof("Certificate cleanup complete. Deleted %d expired/orphaned certificate sets for unmanaged domains", deleted)
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
