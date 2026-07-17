package acp

import (
	"context"
	"fmt"
)

// DefaultClientHandler can be embedded by clients that only implement
// a subset of ACP client-side capabilities.
type DefaultClientHandler struct{}

func (DefaultClientHandler) RequestPermission(ctx context.Context, req *RequestPermissionRequest) (*RequestPermissionResponse, error) {
	return nil, fmt.Errorf("client capability not implemented: session/request_permission")
}

func (DefaultClientHandler) ReadTextFile(ctx context.Context, req *ReadTextFileRequest) (*ReadTextFileResponse, error) {
	return nil, fmt.Errorf("client capability not implemented: fs/read_text_file")
}

func (DefaultClientHandler) WriteTextFile(ctx context.Context, req *WriteTextFileRequest) (*WriteTextFileResponse, error) {
	return nil, fmt.Errorf("client capability not implemented: fs/write_text_file")
}

func (DefaultClientHandler) CreateTerminal(ctx context.Context, req *CreateTerminalRequest) (*CreateTerminalResponse, error) {
	return nil, fmt.Errorf("client capability not implemented: terminal/create")
}

func (DefaultClientHandler) TerminalOutput(ctx context.Context, req *TerminalOutputRequest) (*TerminalOutputResponse, error) {
	return nil, fmt.Errorf("client capability not implemented: terminal/output")
}

func (DefaultClientHandler) ReleaseTerminal(ctx context.Context, req *ReleaseTerminalRequest) (*ReleaseTerminalResponse, error) {
	return nil, fmt.Errorf("client capability not implemented: terminal/release")
}

func (DefaultClientHandler) WaitForTerminalExit(ctx context.Context, req *WaitForTerminalExitRequest) (*WaitForTerminalExitResponse, error) {
	return nil, fmt.Errorf("client capability not implemented: terminal/wait_for_exit")
}

func (DefaultClientHandler) KillTerminal(ctx context.Context, req *KillTerminalRequest) (*KillTerminalResponse, error) {
	return nil, fmt.Errorf("client capability not implemented: terminal/kill")
}
