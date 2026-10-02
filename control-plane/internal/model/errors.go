// Package model holds the domain types and sentinel errors of the control
// plane. It must not import any other internal package.
package model

import "errors"

// Sentinel errors. Handlers translate these to protocol-specific errors;
// callers must identify them with errors.Is.
var (
	// ErrNotFound means the requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means the entity already exists or a state conflict
	// prevents the operation.
	ErrConflict = errors.New("conflict")
	// ErrInvalidState means the state machine rejects the transition.
	ErrInvalidState = errors.New("invalid state transition")
	// ErrLeaseExpired means the task lease has lapsed.
	ErrLeaseExpired = errors.New("lease expired")
	// ErrLeaseHeld means another agent currently owns the lease.
	ErrLeaseHeld = errors.New("lease held by another agent")
	// ErrForbidden means the actor is not allowed to perform the action.
	ErrForbidden = errors.New("forbidden")
	// ErrArgument means the caller supplied an invalid argument.
	ErrArgument = errors.New("invalid argument")
	// ErrStaleExecution means the caller presented a missing, old, or
	// mismatched ingest execution scope.
	ErrStaleExecution = errors.New("stale execution")
	// ErrResourceBusy means a live execution holds a resource and the
	// caller did not ask to replace it.
	ErrResourceBusy = errors.New("resource busy")
	// ErrObsolete means a replayed request no longer matches a live execution.
	ErrObsolete = errors.New("obsolete")
	// ErrInstanceConflict means another runtime instance of the same agent
	// still holds a live execution.
	ErrInstanceConflict = errors.New("instance conflict")
	// ErrProtocolUpgrade means the worker cannot take tasks that require the
	// current execution protocol.
	ErrProtocolUpgrade = errors.New("execution protocol upgrade required")
)
