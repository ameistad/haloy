package certificates

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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

type DomainInfo struct {
	email string
}

type Manager struct {
	config Config
	logger *logrus.Logger
	// Key is domain name to ensure uniqueness
	domains     map[string]DomainInfo
	domainMutex sync.RWMutex

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Client manager for handling ACME clients based on email. One client per email.
	clientManager *ClientManager
}

func NewManager(config Config) (*Manager, error) {
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
		domains:       make(map[string]DomainInfo),
		ctx:           ctx,
		cancel:        cancel,
		clientManager: clientManager,
	}

	return m, nil
}

type DomainEmail struct {
	Domain string
	Email  string
}

func (m *Manager) Start() {
	go m.renewalLoop()
	go m.cleanupLoop()
}

func (m *Manager) Stop() {
	m.cancel()
}

func (m *Manager) AddDomains(pairs []DomainEmail) {
	m.domainMutex.Lock()
	defer m.domainMutex.Unlock()

	for _, pair := range pairs {
		if _, exists := m.domains[pair.Domain]; exists {
			m.logger.Warnf("Domain %s already managed", pair.Domain)
			continue
		}

		m.domains[pair.Domain] = DomainInfo{
			email: pair.Email,
		}
	}
}

func (m *Manager) RemoveDomain(domain string) {
	m.domainMutex.Lock()
	defer m.domainMutex.Unlock()

	delete(m.domains, domain)
}

func (m *Manager) Refresh() {
	m.logger.Infof("Refresh requested for certificate manager")
	m.checkRenewals()
}

// renewalLoop periodically checks for certificates that need renewal
func (m *Manager) renewalLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Do an initial check
	m.checkRenewals()

	for {
		select {
		case <-ticker.C:
			m.checkRenewals()
		case <-m.ctx.Done():
			return
		}
	}
}

// cleanupLoop periodically checks for and removes expired certificates
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Do an initial cleanup
	m.cleanupExpiredCertificates()

	for {
		select {
		case <-ticker.C:
			m.cleanupExpiredCertificates()
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Manager) checkRenewals() {
	m.logger.Infof("Starting certificate renewal check")

	// Debug logging
	m.logger.Infof("Available certificate directories: %s", m.config.CertDir)

	// Copy the current state of m.domains into a local map.
	// This is necessary because holding the lock while doing I/O operations like
	// file stat and certificate parsing could block other goroutines that need to add
	// or remove domains. By working on a copy, we ensure thread-safe read access without
	// holding the lock during potentially long-running operations.
	m.logger.Infof("Acquiring read lock to copy domains")
	m.domainMutex.RLock()
	domains := make(map[string]DomainInfo, len(m.domains))
	maps.Copy(domains, m.domains)
	m.domainMutex.RUnlock()
	m.logger.Infof("Released read lock after copying domains")

	m.logger.Infof("Checking renewals for %d domains", len(domains))
	if len(domains) == 0 {
		m.logger.Infof("No domains to check, renewal check complete")
		return
	}

	for domain, data := range domains {
		m.logger.Infof("Checking domain: %s", domain)
		// Check if certificate exists and needs renewal
		certFile := filepath.Join(m.config.CertDir, domain+".crt")
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			// Certificate doesn't exist, obtain it
			m.logger.Infof("Certificate for %s doesn't exist, obtaining new certificate", domain)
			m.obtainCertificate(domain, data.email)
			continue
		}

		// Load cert to check expiry
		cert, err := tls.LoadX509KeyPair(
			filepath.Join(m.config.CertDir, domain+".crt"),
			filepath.Join(m.config.CertDir, domain+".key"),
		)
		if err != nil {
			m.logger.Errorf("Failed to load certificate for %s: %v", domain, err)
			continue
		}

		// Check if certificate is about to expire (30 days threshold)
		if len(cert.Certificate) == 0 {
			m.logger.Errorf("Invalid certificate for %s", domain)
			continue
		}

		parsedCert, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			m.logger.Errorf("Failed to parse certificate for %s: %v", domain, err)
			continue
		}

		// Renew if certificate expires in less than 30 days
		if time.Until(parsedCert.NotAfter) < 30*24*time.Hour {
			m.logger.Infof("Certificate for %s expires soon, renewing", domain)
			m.obtainCertificate(domain, data.email)
		} else {
			m.logger.Infof("Certificate for %s is valid until %s", domain, parsedCert.NotAfter)
		}
	}
	m.logger.Infof("Certificate renewal check complete")
}

func (m *Manager) obtainCertificate(domain string, email string) {
	m.logger.Infof("Starting certificate obtainment for domain: %s with email: %s", domain, email)

	client, err := m.clientManager.LoadOrRegisterClient(email)
	if err != nil {
		m.logger.Errorf("Failed to load or register client for %s: %v", email, err)
		return
	}
	m.logger.Infof("Successfully loaded ACME client for email: %s", email)

	request := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}

	m.logger.Infof("Requesting certificate from ACME provider for domain: %s", domain)
	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		m.logger.Errorf("Failed to obtain certificate for %s: %v", domain, err)
		return
	}
	m.logger.Infof("Successfully obtained certificate for domain: %s", domain)

	// Save the certificate
	m.logger.Infof("Saving certificate for domain: %s", domain)
	err = m.saveCertificate(domain, certificates)
	if err != nil {
		m.logger.Errorf("Failed to save certificate for %s: %v", domain, err)
		return
	}
	m.logger.Infof("Successfully saved certificate for domain: %s", domain)
}

func (m *Manager) saveCertificate(domain string, cert *certificate.Resource) error {
	// Save certificate
	certPath := filepath.Join(m.config.CertDir, domain+".crt")
	if err := os.WriteFile(certPath, cert.Certificate, 0644); err != nil {
		return fmt.Errorf("failed to save certificate: %w", err)
	}

	// Save private key
	keyPath := filepath.Join(m.config.CertDir, domain+".key")
	if err := os.WriteFile(keyPath, cert.PrivateKey, 0600); err != nil {
		return fmt.Errorf("failed to save private key: %w", err)
	}

	// Create combined file for HAProxy (concatenate cert and key)
	combinedPath := filepath.Join(m.config.CertDir, domain+".crt.key")
	pemContent := append(cert.Certificate, '\n')
	pemContent = append(pemContent, cert.PrivateKey...)

	if err := os.WriteFile(combinedPath, pemContent, 0600); err != nil {
		return fmt.Errorf("failed to save combined certificate: %w", err)
	}

	return nil
}

func (m *Manager) cleanupExpiredCertificates() {
	m.logger.Infof("Starting certificate cleanup check")

	// Read all certificate files in the certificate directory
	files, err := os.ReadDir(m.config.CertDir)
	if err != nil {
		m.logger.Errorf("Failed to read certificate directory: %v", err)
		return
	}

	// Track how many were deleted
	deleted := 0

	// Map for quick domain lookup
	m.domainMutex.RLock()
	managedDomains := make(map[string]struct{}, len(m.domains))
	for domain := range m.domains {
		managedDomains[domain] = struct{}{}
	}
	m.domainMutex.RUnlock()

	// Check each .crt file
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".crt") && !strings.HasSuffix(file.Name(), ".crt.key") {
			// Extract domain name from filename
			domain := strings.TrimSuffix(file.Name(), ".crt")

			// Check if this domain is still being managed
			_, isManaged := managedDomains[domain]

			certPath := filepath.Join(m.config.CertDir, file.Name())
			keyPath := filepath.Join(m.config.CertDir, domain+".key")
			combinedPath := filepath.Join(m.config.CertDir, domain+".crt.key")

			// Check if certificate has expired
			certData, err := os.ReadFile(certPath)
			if err != nil {
				m.logger.Warnf("Failed to read certificate %s: %v", certPath, err)
				continue
			}

			// Parse the certificate
			block, _ := pem.Decode(certData)
			if block == nil || block.Type != "CERTIFICATE" {
				m.logger.Warnf("Failed to decode PEM data for %s", domain)
				continue
			}

			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				m.logger.Warnf("Failed to parse certificate for %s: %v", domain, err)
				continue
			}

			// If certificate is expired AND domain is no longer managed, delete files
			if time.Now().After(cert.NotAfter) && !isManaged {
				m.logger.Infof("Deleting expired certificate for unmanaged domain %s", domain)

				// Remove all certificate files for this domain
				os.Remove(certPath)
				os.Remove(keyPath)
				os.Remove(combinedPath)
				deleted++
			}
		}
	}

	m.logger.Infof("Certificate cleanup complete. Deleted %d expired certificates for unmanaged domains", deleted)
}
