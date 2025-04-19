package logging

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Client represents a connected client with a app name filter.
type Client struct {
	conn          net.Conn
	appNameFilter string
	done          chan struct{}
}

// Server holds state for our TCP server.
type Server struct {
	address         string
	clients         map[string]*Client // Using map for more efficient lookups
	mutex           sync.RWMutex       // Using RWMutex for better concurrency
	logChan         chan string
	listener        net.Listener
	context         context.Context
	waitGroup       sync.WaitGroup
	maxClients      int
	clientSemaphore chan struct{} // Semaphore to limit concurrent connections
}

// NewServer creates a new Server.
func NewServer(ctx context.Context, addr string) *Server {
	return &Server{
		address:         addr,
		clients:         make(map[string]*Client),
		logChan:         make(chan string, 100),
		context:         ctx,
		maxClients:      100,                      // Default max clients
		clientSemaphore: make(chan struct{}, 100), // Match maxClients
	}
}

// SetMaxClients sets the maximum number of concurrent client connections
func (s *Server) SetMaxClients(max int) {
	if max > 0 {
		s.maxClients = max
		// Resize the semaphore
		s.clientSemaphore = make(chan struct{}, max)
	}
}

// Listen starts listening for new client connections.
func (s *Server) Listen() error {
	var err error
	s.listener, err = net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", s.address, err)
	}

	// Start the broadcaster
	s.waitGroup.Add(1)
	go func() {
		defer s.waitGroup.Done()
		s.broadcastLogs()
	}()

	// Accept connections
	s.waitGroup.Add(1)
	go func() {
		defer s.waitGroup.Done()
		for {
			select {
			case <-s.context.Done():
				return
			default:
				s.listener.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
				conn, err := s.listener.Accept()
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						// This is just a timeout for our non-blocking check, continue
						continue
					}
					// Real error
					continue
				}

				// Try to acquire a semaphore slot
				select {
				case s.clientSemaphore <- struct{}{}:
					// Got a slot, proceed with connection
					s.waitGroup.Add(1)
					go func() {
						defer s.waitGroup.Done()
						defer func() { <-s.clientSemaphore }() // Release the slot when done
						s.handleConnection(conn)
					}()
				default:
					// No slots available, reject the connection
					conn.Write([]byte("Server is at maximum capacity. Please try again later.\n"))
					conn.Close()
				}
			}
		}
	}()

	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop() {
	// Close the listener
	if s.listener != nil {
		s.listener.Close()
	}

	// Close the log channel to signal the broadcaster to stop
	close(s.logChan)

	// Close all client connections
	s.mutex.Lock()
	for _, client := range s.clients {
		close(client.done)
		client.conn.Close()
	}
	s.clients = nil
	s.mutex.Unlock()

	// Wait for all goroutines to exit
	s.waitGroup.Wait()
}

// handleConnection processes a new client connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Set read deadline for filter
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Read the filtering criteria (e.g., an app name) from the client.
	reader := bufio.NewReader(conn)
	filter, err := reader.ReadString('\n')
	if err != nil {
		if err != io.EOF {
			fmt.Printf("Error reading filter: %v\n", err)
		}
		return
	}
	filter = strings.TrimSpace(filter)

	// Reset read deadline
	conn.SetReadDeadline(time.Time{})

	// Generate a unique ID for this client
	clientID := fmt.Sprintf("%s-%d", conn.RemoteAddr().String(), time.Now().UnixNano())

	client := &Client{
		conn:          conn,
		appNameFilter: filter,
		done:          make(chan struct{}),
	}

	// Add client to map
	s.mutex.Lock()
	s.clients[clientID] = client
	s.mutex.Unlock()

	// Welcome message
	conn.Write([]byte("Connected to log stream. Filter: " + filter + "\n"))

	// Block until connection terminates or server shuts down
	select {
	case <-s.context.Done():
		return
	case <-client.done:
		return
	}
}

// broadcastLogs delivers log messages to clients matching their specific appName filter.
func (s *Server) broadcastLogs() {
	for {
		select {
		case <-s.context.Done():
			return
		case msg, ok := <-s.logChan:
			if !ok {
				log.Info().Msg("Log channel closed, stopping broadcaster.")
				return
			}

			// Parse the log message as JSON once
			var logEntry map[string]interface{}
			// Use []byte(msg) for unmarshalling
			isJson := json.Unmarshal([]byte(msg), &logEntry) == nil

			s.mutex.RLock()
			clientsCopy := make(map[string]*Client, len(s.clients))
			clientIDs := make([]string, 0, len(s.clients))
			for id, client := range s.clients {
				clientsCopy[id] = client
				clientIDs = append(clientIDs, id)
			}
			s.mutex.RUnlock()

			var disconnectedClients []string
			for _, id := range clientIDs {
				c := clientsCopy[id]
				send := false // Default to not sending

				if c.appNameFilter == "" {
					// Client wants all logs
					send = true
				} else if isJson {
					// Client has a filter AND the log message is valid JSON
					// Check if the "appName" field exists and matches the filter
					if appName, ok := logEntry[LogFieldAppName].(string); ok && appName == c.appNameFilter {
						send = true
					}
				}
				// If !isJson and c.filter != "", send remains false

				if send {
					c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
					// Append newline as Write expects raw bytes including delimiter usually
					_, err := c.conn.Write([]byte(msg + "\n"))
					c.conn.SetWriteDeadline(time.Time{})

					if err != nil {
						log.Warn().Err(err).Str("clientAddr", c.conn.RemoteAddr().String()).Str("filter", c.appNameFilter).Msg("Failed to write to log stream client, marking for removal.")
						disconnectedClients = append(disconnectedClients, id)
						select {
						case <-c.done:
						default:
							close(c.done)
						}
						c.conn.Close()
					}
				}
			}

			if len(disconnectedClients) > 0 {
				s.mutex.Lock()
				for _, id := range disconnectedClients {
					delete(s.clients, id)
				}
				s.mutex.Unlock()
				log.Debug().Strs("disconnectedClients", disconnectedClients).Msg("Removed disconnected clients")
			}
		}
	}
}

// Publish pushes a log message to all clients.
func (s *Server) Publish(msg string) {
	select {
	case s.logChan <- msg:
		// Message sent
	case <-s.context.Done():
		// Server is shutting down
	default:
		// Channel full, log is dropped
	}
}

// LogStreamWriter adapts logstream.Server to io.Writer for zerolog
type LogStreamWriter struct {
	Server  *Server
	Context context.Context
}

func (lsw *LogStreamWriter) Write(p []byte) (n int, err error) {
	// Trim trailing newline added by zerolog ConsoleWriter
	msg := string(bytes.TrimSpace(p))
	if msg != "" {
		lsw.Server.Publish(msg)
	}
	return len(p), nil
}
