package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// writeJSON marshals a value to JSON, sets the Content-Type header,
// writes the status code, and sends the response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")

	// Write the HTTP status code to the response. This must be done before writing the body.
	w.WriteHeader(status)

	// Use json.NewEncoder to stream the JSON response directly to the ResponseWriter.
	// This is more efficient than marshaling to a byte slice first.
	return json.NewEncoder(w).Encode(data)
}

// decodeJSON reads a JSON-encoded value from an io.Reader and decodes it
// into the provided destination value 'v'.
func decodeJSON(r io.Reader, v interface{}) error {
	// DEBUG: Read the body first so we can log it
	body, err := io.ReadAll(r)
	if err != nil {
		return errors.New("failed to read request body")
	}

	// Log the received JSON for debugging
	fmt.Printf("Debug: Received JSON:\n%s\n", string(body))

	// dec := json.NewDecoder(r)
	// Create a new decoder from the body we just read
	dec := json.NewDecoder(strings.NewReader(string(body)))

	// Disallow unknown fields in the JSON. If the client sends a field
	// that doesn't exist in our struct, this will cause an error.
	dec.DisallowUnknownFields()

	// Decode the JSON.
	err = dec.Decode(v)
	if err != nil {
		// Return a more specific error for common cases.
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError

		switch {
		case errors.As(err, &syntaxError):
			return errors.New("request body contains badly-formed JSON")

		case errors.As(err, &unmarshalTypeError):
			return errors.New("request body contains an invalid value for the " + unmarshalTypeError.Field + " field")

		case errors.Is(err, io.EOF):
			return errors.New("request body must not be empty")

		default:
			return err
		}
	}

	return nil
}
