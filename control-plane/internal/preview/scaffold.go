package preview

import "context"

// Unimplemented is a fail-closed framework port. It is NOT registered with API/MCP.
// Agent B replaces this with a transaction-backed application, not successful empty replies.
type Unimplemented struct{}

var _ Application = Unimplemented{}

func (Unimplemented) Start(context.Context, string, string, StartRequest) (View, error) {
	return View{}, ErrNotImplemented
}
func (Unimplemented) List(context.Context, string) (List, error) { return List{}, ErrNotImplemented }
func (Unimplemented) Get(context.Context, string) (View, error)  { return View{}, ErrNotImplemented }
func (Unimplemented) Cancel(context.Context, string, string) (View, error) {
	return View{}, ErrNotImplemented
}
func (Unimplemented) TaskInput(context.Context, string, string) (Input, error) {
	return Input{}, ErrNotImplemented
}
func (Unimplemented) Submit(context.Context, string, string, string) error { return ErrNotImplemented }
func (Unimplemented) ResolveFile(context.Context, string, string) (ResolvedFile, error) {
	return ResolvedFile{}, ErrNotImplemented
}
