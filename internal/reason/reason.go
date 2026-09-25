// Package reason contains Worklease's stable error vocabulary and exit families.
package reason

import (
	"errors"
)

const (
	ExitSuccess      = 0
	ExitInternal     = 1
	ExitOwnership    = 2
	ExitLedger       = 3
	ExitInvalid      = 64
	ExitAuthority    = 75
	ExitChildTimeout = 124
	ExitInterrupted  = 130
)

// Stable reason identifiers. Keep additions in registry below as well.
const (
	ReasonInternal                       = "internal"
	ReasonAlreadyClaimed                 = "already-claimed"
	ReasonStaleClaim                     = "stale-claim"
	ReasonInvalidToken                   = "invalid-token"
	ReasonStaleRevision                  = "stale-revision"
	ReasonClaimExpired                   = "claim-expired"
	ReasonWaitTimeout                    = "wait-timeout"
	ReasonVerifyFailed                   = "verify-failed"
	ReasonOwnershipLost                  = "ownership-lost"
	ReasonHandleInUse                    = "handle-in-use"
	ReasonOperationInProgress            = "operation-in-progress"
	ReasonUnknownOutcomePending          = "unknown-outcome-pending"
	ReasonOperationRequestMismatch       = "operation-request-mismatch"
	ReasonUnknownOutcome                 = "unknown-outcome"
	ReasonExpectedHashMismatch           = "expected-hash-mismatch"
	ReasonReconciliationConflict         = "reconciliation-conflict"
	ReasonOperationAmbiguous             = "operation-ambiguous"
	ReasonOperationNotFound              = "operation-not-found"
	ReasonReplayExpired                  = "replay-expired"
	ReasonInvalidArgument                = "invalid-argument"
	ReasonConfigInvalid                  = "config-invalid"
	ReasonConfigMissing                  = "config-missing"
	ReasonNoSourcesConfigured            = "no-sources-configured"
	ReasonClaimSelectionMissing          = "claim-selection-missing"
	ReasonResourceInputConflict          = "resource-input-conflict"
	ReasonInvalidResource                = "invalid-resource"
	ReasonUnknownPolicy                  = "unknown-policy"
	ReasonInvalidPath                    = "invalid-path"
	ReasonHandleUnsafe                   = "handle-unsafe"
	ReasonHandleMalformed                = "handle-malformed"
	ReasonCredentialUnsafe               = "credential-unsafe"
	ReasonCredentialMalformed            = "credential-malformed"
	ReasonCredentialSourceConflict       = "credential-source-conflict"
	ReasonAgentIDRequired                = "agent-id-required"
	ReasonUnsupportedCoordinationReplace = "unsupported-coordination-replace"
	ReasonCursorInvalid                  = "cursor-invalid"
	ReasonQueueIncomplete                = "incomplete"
	ReasonSetupConfigMalformed           = "setup-config-malformed"
	ReasonHomeUnsafe                     = "home-unsafe"
	ReasonStorageFailure                 = "storage-failure"
	ReasonSchemaUnsupported              = "schema-unsupported"
	ReasonSchemaCorrupt                  = "schema-corrupt"
	ReasonHostedHomeRequiresRemote       = "hosted-home-requires-remote"
	ReasonHostedLockHeld                 = "hosted-lock-held"
	ReasonHandleWriteFailed              = "handle-write-failed"
	ReasonAuthorityMismatch              = "authority-mismatch"
	ReasonAuthorityRestored              = "authority-restored"
	ReasonAuthenticationRequired         = "authentication-required"
	ReasonInstallationRevoked            = "installation-revoked"
	ReasonAuthorizationDenied            = "authorization-denied"
	ReasonResourceNotEnrolled            = "resource-not-enrolled"
	ReasonRecoveryRequired               = "recovery-required"
	ReasonRecoveryClosed                 = "recovery-closed"
	ReasonInviteInvalid                  = "invite-invalid"
	ReasonInviteExpired                  = "invite-expired"
	ReasonInviteUsed                     = "invite-used"
	ReasonClockRegression                = "clock-regression"
	ReasonChildTimeout                   = "child-timeout"
	ReasonInterrupted                    = "interrupted"
	ReasonHookInputInvalid               = "hook-input-invalid"
	ReasonProtocolVersionUnsupported     = "protocol-version-unsupported"
	ReasonRateLimited                    = "rate-limited"
	ReasonRequestTooLarge                = "request-too-large"
	ReasonResponseTooLarge               = "response-too-large"
	ReasonRemoteTransportFailure         = "remote-transport-failure"
	ReasonOperationKindUnsupported       = "operation-kind-unsupported"
	ReasonCancelled                      = "cancelled"
)

// Error is a structured application error. Details must contain only
// non-secret metadata; output also applies defense-in-depth redaction.
type Error struct {
	Reason  string         `json:"reason"`
	Code    int            `json:"exitCode"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func New(name, message string) *Error {
	return &Error{Reason: name, Code: CodeFor(name), Message: message}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *Error) ExitCode() int {
	if e == nil {
		return ExitInternal
	}
	return e.Code
}

func (e *Error) With(key string, value any) *Error {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// Unwrap deliberately returns nil: Error is the public classification and
// never exposes an underlying error that could contain credentials.
func (e *Error) Unwrap() error { return nil }

func CodeFor(name string) int {
	if code, ok := registry[name]; ok {
		return code
	}
	return ExitInternal
}

func Registered(name string) bool { _, ok := registry[name]; return ok }

func As(err error) *Error {
	var classified *Error
	if errors.As(err, &classified) {
		return classified
	}
	return nil
}

func Invalid(message string) *Error { return New(ReasonInvalidArgument, message) }

var registry = map[string]int{
	ReasonInternal:       ExitInternal,
	ReasonAlreadyClaimed: ExitOwnership, ReasonStaleClaim: ExitOwnership, ReasonInvalidToken: ExitOwnership,
	ReasonStaleRevision: ExitOwnership, ReasonClaimExpired: ExitOwnership, ReasonWaitTimeout: ExitOwnership,
	ReasonVerifyFailed: ExitOwnership, ReasonOwnershipLost: ExitOwnership, ReasonHandleInUse: ExitOwnership,
	ReasonOperationInProgress: ExitOwnership, ReasonUnknownOutcomePending: ExitOwnership,
	ReasonOperationRequestMismatch: ExitLedger, ReasonUnknownOutcome: ExitLedger, ReasonExpectedHashMismatch: ExitLedger,
	ReasonReconciliationConflict: ExitLedger, ReasonOperationAmbiguous: ExitLedger, ReasonOperationNotFound: ExitLedger,
	ReasonReplayExpired:   ExitLedger,
	ReasonInvalidArgument: ExitInvalid, ReasonConfigInvalid: ExitInvalid, ReasonConfigMissing: ExitInvalid, ReasonNoSourcesConfigured: ExitInvalid,
	ReasonClaimSelectionMissing: ExitInvalid, ReasonResourceInputConflict: ExitInvalid, ReasonInvalidResource: ExitInvalid,
	ReasonUnknownPolicy: ExitInvalid, ReasonInvalidPath: ExitInvalid, ReasonHandleUnsafe: ExitInvalid,
	ReasonHandleMalformed: ExitInvalid, ReasonCredentialUnsafe: ExitInvalid, ReasonCredentialMalformed: ExitInvalid,
	ReasonCredentialSourceConflict: ExitInvalid, ReasonAgentIDRequired: ExitInvalid,
	ReasonUnsupportedCoordinationReplace: ExitInvalid, ReasonCursorInvalid: ExitInvalid, ReasonQueueIncomplete: ExitInvalid,
	ReasonSetupConfigMalformed: ExitInvalid, ReasonHookInputInvalid: ExitInvalid,
	ReasonProtocolVersionUnsupported: ExitInvalid, ReasonRateLimited: ExitInvalid,
	ReasonRequestTooLarge: ExitInvalid, ReasonResponseTooLarge: ExitInvalid,
	ReasonRemoteTransportFailure:   ExitAuthority,
	ReasonOperationKindUnsupported: ExitInvalid, ReasonCancelled: ExitInterrupted,
	ReasonHomeUnsafe: ExitAuthority, ReasonStorageFailure: ExitAuthority, ReasonSchemaUnsupported: ExitAuthority,
	ReasonSchemaCorrupt: ExitAuthority, ReasonHostedHomeRequiresRemote: ExitAuthority, ReasonHostedLockHeld: ExitAuthority,
	ReasonHandleWriteFailed: ExitAuthority,
	ReasonAuthorityMismatch: ExitAuthority, ReasonAuthorityRestored: ExitAuthority,
	ReasonAuthenticationRequired: ExitOwnership, ReasonInstallationRevoked: ExitOwnership,
	ReasonAuthorizationDenied: ExitOwnership, ReasonResourceNotEnrolled: ExitInvalid,
	ReasonRecoveryRequired: ExitLedger, ReasonRecoveryClosed: ExitLedger,
	ReasonInviteInvalid: ExitOwnership, ReasonInviteExpired: ExitOwnership, ReasonInviteUsed: ExitOwnership,
	ReasonClockRegression: ExitAuthority,
	ReasonChildTimeout:    ExitChildTimeout, ReasonInterrupted: ExitInterrupted,
}

// DefinitiveNoCommit reports whether err proves the requested mutation did
// not commit. Uncertain outcomes, storage failures, expired replay windows,
// lost ownership during a guard, interruptions, and unclassified errors all
// return false: the caller must keep its pending request for exact recovery.
func DefinitiveNoCommit(err error) bool {
	e := As(err)
	if e == nil {
		return false
	}
	switch e.Reason {
	case ReasonUnknownOutcome, ReasonUnknownOutcomePending, ReasonReplayExpired, ReasonStorageFailure, ReasonHandleWriteFailed, ReasonOwnershipLost, ReasonChildTimeout, ReasonInterrupted, ReasonRemoteTransportFailure:
		return false
	}
	return true
}

// OwnershipRenewalFailure reports whether a guard's internal renewal error is
// an ownership or clock failure that must terminate the supervised child. Other
// errors are transient and the guard keeps running until its lease deadline.
func OwnershipRenewalFailure(err error) bool {
	e := As(err)
	if e == nil {
		return false
	}
	switch e.Reason {
	case ReasonStaleClaim, ReasonInvalidToken, ReasonClaimExpired, ReasonStaleRevision, ReasonOwnershipLost, ReasonClockRegression, ReasonAuthorityMismatch, ReasonAuthorityRestored, ReasonAuthenticationRequired, ReasonInstallationRevoked, ReasonAuthorizationDenied, ReasonOperationNotFound:
		return true
	}
	return false
}
