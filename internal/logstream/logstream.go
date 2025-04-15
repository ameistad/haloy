package logstream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Client represents a connected client with a filter.
type Client struct {
	conn   net.Conn
	filter string
	done   chan struct{}
}

// Server holds state for our TCP server.
type Server struct {
	addr            string
	clients         map[string]*Client // Using map for more efficient lookups
	mu              sync.RWMutex       // Using RWMutex for better concurrency
	logCh           chan string
	listener        net.Listener
	ctx             context.Context
	waitGroup       sync.WaitGroup
	maxClients      int
	clientSemaphore chan struct{} // Semaphore to limit concurrent connections
}

// NewServer creates a new Server.
func NewServer(ctx context.Context, addr string) *Server {
	return &Server{
		addr:            addr,
		clients:         make(map[string]*Client),
		logCh:           make(chan string, 100),
		ctx:             ctx,
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
	s.listener, err = net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", s.addr, err)
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
			case <-s.ctx.Done():
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
	close(s.logCh)

	// Close all client connections
	s.mu.Lock()
	for _, client := range s.clients {
		close(client.done)
		client.conn.Close()
	}
	s.clients = nil
	s.mu.Unlock()

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
		conn:   conn,
		filter: filter,
		done:   make(chan struct{}),
	}

	// Add client to map
	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()

	// Welcome message
	conn.Write([]byte("Connected to log stream. Filter: " + filter + "\n"))

	// Block until connection terminates or server shuts down
	select {
	case <-s.ctx.Done():
		return
	case <-client.done:
		return
	}
}

// broadcastLogs delivers log messages to all clients matching their filter.
func (s *Server) broadcastLogs() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-s.logCh:
			if !ok {
				// Channel closed, exit
				return
			}

			// Use RLock for reading clients
			s.mu.RLock()
			// Create a copy of clients to avoid holding the lock during writes
			clientsCopy := make(map[string]*Client, len(s.clients))
			for id, client := range s.clients {
				clientsCopy[id] = client
			}
			s.mu.RUnlock()

			// Process clients without holding the lock
			var disconnectedClients []string
			for id, c := range clientsCopy {
				// If filter is empty or message contains filter
				if c.filter == "" || strings.Contains(msg, c.filter) {
					_, err := c.conn.Write([]byte(msg + "\n"))
					if err != nil {
						// Mark client for removal
						disconnectedClients = append(disconnectedClients, id)
						close(c.done)
						c.conn.Close()
					}
				}
			}

			// Remove disconnected clients if any
			if len(disconnectedClients) > 0 {
				s.mu.Lock()
				for _, id := range disconnectedClients {
					delete(s.clients, id)
				}
				s.mu.Unlock()
			}
		}
	}
}

// Publish pushes a log message to all clients.
func (s *Server) Publish(msg string) {
	select {
	case s.logCh <- msg:
		// Message sent
	case <-s.ctx.Done():
		// Server is shutting down
	default:
		// Channel full, log is dropped
	}
}

// LogrusHook is a logrus hook that sends logs to connected clients
type LogrusHook struct {
	server *Server
}

// NewLogrusHook creates a new logrus hook for the log server
func NewLogrusHook(server *Server) *LogrusHook {
	return &LogrusHook{server: server}
}

// Fire implements logrus.Hook.Fire
func (h *LogrusHook) Fire(entry *logrus.Entry) error {
	line, err := entry.String()
	if err != nil {
		return err
	}
	h.server.Publish(line)
	return nil
}

// Levels implements logrus.Hook.Levels
func (h *LogrusHook) Levels() []logrus.Level {
	return logrus.AllLevels
}
