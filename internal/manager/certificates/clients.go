package certificates

import (
	"crypto"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

type User struct {
	Email        string
	Registration *registration.Resource
	privateKey   crypto.PrivateKey
}

func (u *User) GetEmail() string {
	return u.Email
}
func (u *User) GetRegistration() *registration.Resource {
	return u.Registration
}
func (u *User) GetPrivateKey() crypto.PrivateKey {
	return u.privateKey
}

type ClientManager struct {
	tlsStaging         bool
	keyManager         *KeyManager
	clients            map[string]*lego.Client
	clientsMutex       sync.RWMutex
	sharedHTTPProvider *http01.ProviderServer
}

func NewClientManager(certDir string, tlsStaging bool, httpProviderPort string) (*ClientManager, error) {
	keyDir := filepath.Join(certDir, "accounts")
	keyManager, err := NewKeyManager(keyDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create key manager: %w", err)
	}

	httpProvider := http01.NewProviderServer("", httpProviderPort)

	return &ClientManager{
		tlsStaging:         tlsStaging,
		clients:            make(map[string]*lego.Client),
		keyManager:         keyManager,
		sharedHTTPProvider: httpProvider,
	}, nil
}

func (cm *ClientManager) LoadOrRegisterClient(email string) (*lego.Client, error) {

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

	user := &User{
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
