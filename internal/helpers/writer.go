package helpers

import (
	"bytes"
	"io"
)

// PrefixWriter is an io.Writer implementation that adds a prefix to each line.
// It buffers incomplete lines until a newline is received to ensure the prefix
// is only added at the beginning of complete lines.
type PrefixWriter struct {
	writer io.Writer
	prefix []byte
	buf    bytes.Buffer // Buffer to hold incomplete lines
}

func NewPrefixWriter(writer io.Writer, prefix string) *PrefixWriter {
	return &PrefixWriter{
		writer: writer,
		prefix: []byte(prefix),
	}
}

// Write implements the io.Writer interface. It buffers input until complete lines
// are available, then writes each line with the configured prefix. Incomplete lines
// are stored in the buffer until more data arrives or the writer is closed.
func (pw *PrefixWriter) Write(p []byte) (n int, err error) {
	// Append new data to buffer
	pw.buf.Write(p)

	// Process complete lines from buffer
	for {
		line, err := pw.buf.ReadBytes('\n')
		if err != nil {
			// If error is EOF, put the remaining bytes back in buffer
			if err == io.EOF {
				pw.buf.Write(line) // Write back the incomplete line
				break
			}
			return n, err // Return other errors
		}

		// Write prefix and line
		_, wErr := pw.writer.Write(pw.prefix)
		if wErr != nil {
			return n, wErr
		}
		_, wErr = pw.writer.Write(line)
		if wErr != nil {
			return n, wErr
		}
	}

	return len(p), nil // Report that all input bytes were processed
}
