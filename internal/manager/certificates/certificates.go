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
	"github.com/rs/zerolog"
)

type Config struct {
	CertDir          string
	HTTPProviderPort string
	Logger           zerolog.Logger
	TlsStaging       bool
}

type ManagedDomain struct {
	Canonical string
	Aliases   []string
	Email     string
}

type Manager struct {
	config        Config
	logger        zerolog.Logger
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
	// Create a new logger if none was provided
	if config.Logger.GetLevel() == zerolog.Disabled {
		config.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
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
				m.logger.Info().
					Str("domain", md.Canonical).
					Msg("Updating managed domain info for (Email or Aliases changed)")
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
			m.logger.Info().
				Str("domain", domain).
				Msg("Domain is no longer managed, removing.")
			removed++
		}
	}

	// Log summary of changes
	if added > 0 || updated > 0 || removed > 0 {
		m.logger.Info().
			Int("added", added).
			Int("updated", updated).
			Int("removed", removed).
			Msg("Certificate domains updated")
		// Trigger a refresh check if changes occurred
		go m.Refresh() // Run in background to avoid blocking caller
	} else {
		m.logger.Debug().
			Msg("AddDomains called, but no changes detected in managed domains.")
	}
}

func (m *Manager) Refresh() {
	m.logger.Info().
		Msg("Refresh requested for certificate manager")
	m.refreshMutex.Lock()
	defer m.refreshMutex.Unlock()
	m.refreshNeeded = true

	// If a timer is already running, reset it
	if m.refreshTimer != nil {
		m.logger.Debug().
			Msg("Resetting existing debounce timer")
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
			m.logger.Info().
				Msg("Debounced refresh triggered: running checkRenewals")
			m.checkRenewals()
		} else {
			m.logger.Info().
				Msg("Debounced refresh timer fired, but no longer needed.")
		}
	})
	m.logger.Info().
		Msg("Refresh debouncer timer started/reset.")
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
			m.logger.Info().
				Msg("Running periodic renewal check")
			m.checkRenewals()
		case <-m.ctx.Done():
			m.logger.Info().
				Msg("Renewal loop stopping due to context cancellation.")
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
			m.logger.Info().
				Msg("Running periodic certificate cleanup check.")
			m.cleanupExpiredCertificates()
		case <-m.ctx.Done():
			m.logger.Info().
				Msg("Cleanup loop stopping due to context cancellation.")
			return
		}
	}
}

func (m *Manager) checkRenewals() {
	// --- Lock to prevent concurrent checks ---
	m.checkMutex.Lock()
	m.logger.Debug().
		Msg("Acquired renewal check lock")
	defer func() {
		m.logger.Debug().
			Msg("Releasing renewal check lock")
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

	m.logger.Info().
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
				m.logger.Error().
					Str("domain", domain).
					Err(err).
					Msg("Failed to read certificate file")
				// If we can't read the .crt file, we can't check expiry/SANs.
				// Should we attempt to obtain? Maybe, if the combined file exists but .crt is bad.
				// Let's attempt obtain if reading fails, as something is wrong.
				m.logger.Warn().
					Str("domain", domain).
					Msg("Marking for obtainment due to error reading .crt file.")
				needsObtain = true // Treat read error as needing obtainment
			} else {
				// Use the local parseCertificate helper
				parsedCert, err := parseCertificate(certData)
				if err != nil {
					m.logger.Error().
						Str("domain", domain).
						Err(err).
						Msg("Failed to parse certificate")
					// Treat parse error as needing obtainment
					m.logger.Warn().
						Str("domain", domain).
						Msg("Marking for obtainment due to error parsing certificate.")
					needsObtain = true
				} else {
					// Check expiry
					if time.Until(parsedCert.NotAfter) < 30*24*time.Hour {
						m.logger.Info().
							Str("domain", domain).
							Time("expiry", parsedCert.NotAfter).
							Msg("Certificate expires soon")
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
						m.logger.Info().
							Str("domain", domain).
							Msg("Certificate SAN list needs update. Required:")
						m.logger.Debug().
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
			m.logger.Info().
				Str("domain", domain).
				Bool("obtain_needed", needsObtain).
				Bool("expiry_near", needsRenewalDueToExpiry).
				Bool("san_mismatch", sanMismatch).
				Msg("Triggering certificate obtain/renewal")
			// Pass the full info needed for the request
			m.obtainCertificate(managedDomainInfo)
		} else if !os.IsNotExist(err) { // Only log skipping if we actually checked a cert file
			m.logger.Debug().
				Str("domain", domain).
				Msg("Skipping certificate renewal. It's valid and SANs match.")
		}
	} // end loop over domains
	m.logger.Info().
		Msg("Finished checking renewals.")
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

	m.logger.Info().
		Str("domain", canonicalDomain).
		Str("email", email).
		Strs("domains", allDomains).
		Msg("Starting certificate obtainment/renewal")

	client, err := m.clientManager.LoadOrRegisterClient(email)
	if err != nil {
		m.logger.Error().
			Str("domain", canonicalDomain).
			Err(err).
			Msg("Failed to load or register ACME client")
		return
	}
	m.logger.Info().
		Str("domain", canonicalDomain).
		Msg("Successfully loaded ACME client")

	request := certificate.ObtainRequest{
		Domains: allDomains, // Request cert for canonical + aliases
		Bundle:  true,       // Bundle intermediate certs
	}

	m.logger.Info().
		Str("domain", canonicalDomain).
		Strs("domains", allDomains).
		Msg("Requesting certificate from ACME provider")
	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		m.logger.Error().
			Str("domain", canonicalDomain).
			Err(err).
			Strs("domains", allDomains).
			Msg("Failed to obtain certificate for domains")
		return
	}
	m.logger.Info().
		Str("domain", canonicalDomain).
		Strs("domains", allDomains).
		Msg("Successfully obtained certificate for domains")

	m.logger.Info().
		Str("domain", canonicalDomain).
		Msg("Saving certificate using canonical name")
	err = m.saveCertificate(canonicalDomain, certificates)
	if err != nil {
		m.logger.Error().
			Str("domain", canonicalDomain).
			Err(err).
			Msg("Failed to save certificate")
		return
	}
	m.logger.Info().
		Str("domain", canonicalDomain).
		Msg("Successfully saved certificate")

	// --- Signal Main Loop ---
	m.logger.Info().
		Str("domain", canonicalDomain).
		Msg("Signaling for HAProxy config update after obtaining certificate")
	// Send canonical domain name non-blockingly (in case channel buffer full or receiver slow)
	select {
	case m.updateSignal <- canonicalDomain:
		m.logger.Debug().
			Str("domain", canonicalDomain).
			Msg("Successfully signaled update")
	default:
		m.logger.Warn().
			Str("domain", canonicalDomain).
			Msg("Update signal channel full or closed, skipping signal")
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

	m.logger.Debug().
		Str("domain", domain).
		Str("cert_path", certPath).
		Str("key_path", keyPath).
		Str("combined_path", combinedPath).
		Msg("Saved certificate files")
	return nil
}

func (m *Manager) cleanupExpiredCertificates() {
	m.logger.Info().
		Msg("Starting certificate cleanup check")

	files, err := os.ReadDir(m.config.CertDir)
	if err != nil {
		m.logger.Error().
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
					m.logger.Warn().
						Str("combined_path", combinedPath).
						Str("domain", domain).
						Msg("Found orphaned combined file for unmanaged domain (.crt missing). Deleting.")
					os.Remove(combinedPath)
					os.Remove(keyPath) // Try removing .key too if it exists
					deleted++
				} else if !os.IsNotExist(err) {
					// Log other read errors
					m.logger.Warn().
						Err(err).
						Str("cert_path", certPath).
						Msg("Failed to read certificate file during cleanup")
				}
				continue // Skip if we can't read the cert
			}

			// Use the local parseCertificate helper
			parsedCert, err := parseCertificate(certData)
			if err != nil {
				m.logger.Warn().
					Err(err).
					Str("cert_path", certPath).
					Msg("Failed to parse certificate during cleanup")
				continue // Skip if parsing fails
			}

			// Delete if expired AND unmanaged
			if time.Now().After(parsedCert.NotAfter) && !isManaged {
				m.logger.Info().
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

	m.logger.Info().
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
