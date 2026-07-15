package acp

import "io"

// DialInProcess connects an ACP client to a handler in the same process.
//
// It preserves the JSON-RPC ACP boundary while avoiding a subprocess and stdio.
// This is useful for embedding an agent runtime in a GUI or service process.
func DialInProcess(handler Handler) *Client {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	server := NewServerWithWriter(handler, serverWriter)
	go func() {
		_ = server.ServeFromReader(serverReader)
		_ = serverWriter.Close()
	}()

	return NewClient(clientWriter, clientReader)
}
