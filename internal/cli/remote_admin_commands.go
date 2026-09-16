package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
)

func remoteAdminCommands(s *boundary) []*urfave.Command {
	command := func(name, usage string, flags []urfave.Flag, action func(context.Context, *urfave.Command) error) *urfave.Command {
		return &urfave.Command{Name: name, Usage: usage, UsageText: "worklease " + name, Description: usage + ".", Flags: flags, Action: action, OnUsageError: func(_ context.Context, cmd *urfave.Command, _ error, _ bool) error {
			return s.handle(cmd, reason.Invalid("invalid command-line arguments"))
		}}
	}
	mutating := func() []urfave.Flag {
		return []urfave.Flag{&urfave.StringFlag{Name: "operation-id", Usage: "32-hex replay operation `ID`"}, &urfave.StringFlag{Name: "request-not-after", Usage: "RFC3339 replay deadline `TIME`, at most 24h ahead"}}
	}
	inviteFlags := append(mutating(), &urfave.StringFlag{Name: "role", Usage: "invite role: read, write, or admin"}, &urfave.StringFlag{Name: "invite-file", Usage: "owner-private output `FILE` for the invite secret"}, &urfave.IntFlag{Name: "invite-fd", Usage: "inherited output descriptor `N` for the invite secret", HideDefault: true}, &urfave.StringFlag{Name: "label", Usage: "public invite label `TEXT`"}, &urfave.StringFlag{Name: "expires-at", Usage: "optional RFC3339 invite expiry `TIME`"})
	inviteIssue := command("issue", "issue a remote installation invite", inviteFlags, inviteIssueAction(s))
	invite := command("invite", "manage remote invitations", nil, nil)
	invite.Commands = []*urfave.Command{inviteIssue}
	invite.Description = "Manage remote installation invitations.\n\nExamples:\n  worklease invite issue --profile team --role write --invite-file invite.secret"

	installationList := command("list", "list remote installations", []urfave.Flag{&urfave.BoolFlag{Name: "include-revoked", Usage: "include revoked installations"}}, installationListAction(s))
	installationList.Aliases = []string{"ls"}
	installationRevoke := command("revoke", "revoke a remote installation", append(mutating(), &urfave.StringFlag{Name: "installation-id", Usage: "installation `ID` to revoke"}, &urfave.StringFlag{Name: "reason", Usage: "public revocation `REASON`"}), installationRevokeAction(s))
	installation := command("installation", "manage remote installations", nil, nil)
	installation.Commands = []*urfave.Command{installationList, installationRevoke}
	installation.Description = "Manage remote installations.\n\nExamples:\n  worklease installation list --profile team"

	claimRevoke := command("revoke", "administratively revoke a remote claim", append(mutating(), &urfave.StringFlag{Name: "claim-id", Usage: "claim `ID` to revoke"}, &urfave.StringFlag{Name: "reason", Usage: "public revocation `REASON`"}), claimRevokeAction(s))
	claim := command("claim", "administer remote claims", nil, nil)
	claim.Commands = []*urfave.Command{claimRevoke}
	claim.Description = "Administer remote claims.\n\nExamples:\n  worklease claim revoke --profile team --claim-id ID"

	recoveryStatus := command("status", "show remote recovery state", nil, recoveryStatusAction(s))
	recoveryReopen := command("reopen", "reopen remote admission after restore", append(mutating(), &urfave.Int64Flag{Name: "expected-recovery-revision", Usage: "expected recovery revision `N`", HideDefault: true}, &urfave.StringFlag{Name: "attestation-file", Usage: "private structured attestation `FILE`"}), recoveryReopenAction(s))
	recovery := command("recovery", "inspect or reopen remote recovery", nil, nil)
	recovery.Commands = []*urfave.Command{recoveryStatus, recoveryReopen}
	recovery.Description = "Inspect or reopen remote recovery.\n\nExamples:\n  worklease recovery status --profile team"
	return []*urfave.Command{invite, installation, claim, recovery}
}

func remoteBackend(ctx context.Context, s *boundary, cmd *urfave.Command) (*authorityContext, error) {
	backend, err := authorityFor(ctx, cmd, true)
	if err != nil {
		return nil, s.handle(cmd, err)
	}
	if !backend.Remote {
		backend.Close()
		return nil, s.handle(cmd, reason.New(reason.ReasonConfigMissing, "a remote profile is required"))
	}
	return backend, nil
}

func adminCall(ctx context.Context, backend *authorityContext, path, kind string, mutating bool, operationID string, body map[string]any) (json.RawMessage, error) {
	body["protocolVersion"] = "worklease-http/1"
	body["authorityId"] = backend.Profile.AuthorityID
	body["expectedRestoreId"] = backend.Profile.RestoreID
	if mutating {
		if _, err := backend.HTTP.Metadata(ctx); err != nil {
			return nil, err
		}
		if deadline, ok := body["requestNotAfter"].(time.Time); !ok || deadline.IsZero() {
			deadline, err := backend.HTTP.Clock().RequestNotAfter()
			if err != nil {
				return nil, reason.New(reason.ReasonClockRegression, "authority time is unsampled")
			}
			body["requestNotAfter"] = deadline
		}
		if operationID == "" {
			operationID = randomHex(16)
		}
		body["operationId"] = operationID
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, reason.Invalid("remote request is invalid")
	}
	response, err := backend.API.Execute(ctx, authority.RequestSpec{Path: path, Kind: kind, RequestID: operationID, Body: encoded, Mutating: mutating, Terminal: mutating})
	return response.Result, err
}

func adminDeadline(cmd *urfave.Command) (time.Time, error) {
	if strings.TrimSpace(cmd.String("request-not-after")) == "" {
		return time.Time{}, nil
	}
	return requestDeadlineCLI(cmd)
}

func writeAdminResult(s *boundary, cmd *urfave.Command, operation string, raw json.RawMessage) error {
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		return s.handle(cmd, reason.New(reason.ReasonInvalidArgument, "remote result is invalid"))
	}
	if s.jsonRequested(cmd) {
		if fields, ok := result.(map[string]any); ok {
			return output.WriteSuccess(s.writer, operation, fields)
		}
		return output.WriteSuccess(s.writer, operation, map[string]any{"result": result})
	}
	_, err := fmt.Fprintf(s.writer, "%s completed\n", operation)
	return err
}

type inviteIssueStage struct {
	Artifact        string    `json:"artifact"`
	InviteID        string    `json:"inviteId"`
	OperationID     string    `json:"operationId"`
	Role            string    `json:"role"`
	Label           string    `json:"label"`
	ExpiresAt       string    `json:"expiresAt"`
	RequestNotAfter time.Time `json:"requestNotAfter"`
}

func inviteIssueStagePath(backend *authorityContext) string {
	return filepath.Join(backend.Config.Home, "invite-staging", backend.ProfileName+".json")
}

func loadInviteIssueStage(path string) (inviteIssueStage, error) {
	data, err := handle.ReadOwnerPrivate(path, 64*1024)
	if err != nil {
		return inviteIssueStage{}, err
	}
	var stage inviteIssueStage
	if err := json.Unmarshal(data, &stage); err != nil || stage.Artifact == "" || stage.InviteID == "" || stage.OperationID == "" || stage.Role == "" || stage.Label == "" || stage.RequestNotAfter.IsZero() {
		return inviteIssueStage{}, reason.New(reason.ReasonCredentialUnsafe, "staged invite issue is malformed")
	}
	if _, err := authority.DecodeInviteArtifact(stage.Artifact); err != nil {
		return inviteIssueStage{}, err
	}
	return stage, nil
}

func writeInviteIssueStage(path string, stage inviteIssueStage) error {
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.Marshal(stage)
	if err != nil {
		return err
	}
	return handle.WriteOwnerPrivateNoReplace(path, data, 64*1024)
}

func sameInviteIssueInputs(stage inviteIssueStage, operationID, role, label, expiresAt string, deadline time.Time) bool {
	return (operationID == "" || operationID == stage.OperationID) && stage.Role == role && stage.Label == label && stage.ExpiresAt == expiresAt && stage.RequestNotAfter.Equal(deadline)
}

func sha256SumInvite(artifact string) []byte {
	decoded, _ := authority.DecodeInviteArtifact(artifact)
	sum := sha256.Sum256([]byte(decoded.Invite))
	return sum[:]
}

func publishInviteIssue(stage inviteIssueStage, file string, fd int, fdSet bool) error {
	data := []byte(stage.Artifact + "\n")
	if file != "" {
		if existing, err := handle.ReadOwnerPrivate(file, authority.MaxInviteArtifactBytes+1); err == nil {
			if string(existing) == string(data) {
				return nil
			}
			return reason.New(reason.ReasonCredentialUnsafe, "invite artifact output already exists with different content")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return handle.WriteOwnerPrivateNoReplace(file, data, authority.MaxInviteArtifactBytes+1)
	}
	if !fdSet {
		return reason.New(reason.ReasonCredentialSourceConflict, "invite output source is required")
	}
	return handle.WriteBoundedFD(fd, data, authority.MaxInviteArtifactBytes+1)
}

func inviteIssueAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		file, fdSet := strings.TrimSpace(cmd.String("invite-file")), cmd.IsSet("invite-fd")
		if (file != "") == fdSet {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "exactly one invite output is required"))
		}
		backend, err := remoteBackend(ctx, s, cmd)
		if err != nil {
			return err
		}
		defer backend.Close()
		role, label, expiry := strings.TrimSpace(cmd.String("role")), strings.TrimSpace(cmd.String("label")), strings.TrimSpace(cmd.String("expires-at"))
		if role == "" {
			role = "write"
		}
		if label == "" {
			label = backend.ProfileName
		}
		explicitOperation := strings.TrimSpace(cmd.String("operation-id"))
		explicitDeadline, err := adminDeadline(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		stagePath := inviteIssueStagePath(backend)
		stage, stageErr := loadInviteIssueStage(stagePath)
		if stageErr == nil {
			stagedArtifact, artifactErr := authority.DecodeInviteArtifact(stage.Artifact)
			if artifactErr != nil || stagedArtifact.Endpoint != backend.Profile.Endpoint || stagedArtifact.AuthorityID != backend.Profile.AuthorityID || stagedArtifact.CertificateSHA256 != backend.Profile.CertificateSHA256 {
				return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "staged invite issue trust conflicts with profile"))
			}
			deadline := explicitDeadline
			if deadline.IsZero() {
				deadline = stage.RequestNotAfter
			}
			if !sameInviteIssueInputs(stage, explicitOperation, role, label, expiry, deadline) {
				return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "staged invite issue conflicts with requested inputs"))
			}
		} else if !errors.Is(stageErr, os.ErrNotExist) {
			return s.handle(cmd, stageErr)
		} else {
			if explicitDeadline.IsZero() {
				if _, err := backend.HTTP.Metadata(ctx); err != nil {
					return s.handle(cmd, err)
				}
				explicitDeadline, err = backend.HTTP.Clock().RequestNotAfter()
				if err != nil {
					return s.handle(cmd, reason.New(reason.ReasonClockRegression, "authority time is unsampled"))
				}
			}
			operationID := explicitOperation
			if operationID == "" {
				operationID = randomHex(16)
			}
			secret, inviteID := randomHex(32), randomHex(16)
			artifact, artifactErr := authority.EncodeInviteArtifact(authority.InviteArtifact{Endpoint: backend.Profile.Endpoint, AuthorityID: backend.Profile.AuthorityID, CertificateSHA256: backend.Profile.CertificateSHA256, ProfileHint: label, Invite: secret})
			if artifactErr != nil {
				return s.handle(cmd, artifactErr)
			}
			stage = inviteIssueStage{Artifact: artifact, InviteID: inviteID, OperationID: operationID, Role: role, Label: label, ExpiresAt: expiry, RequestNotAfter: explicitDeadline}
			if err := writeInviteIssueStage(stagePath, stage); err != nil {
				return s.handle(cmd, reason.New(reason.ReasonStorageFailure, "invite issue could not be durably staged"))
			}
		}
		body := map[string]any{"inviteId": stage.InviteID, "role": stage.Role, "label": stage.Label, "expiresAt": stage.ExpiresAt, "inviteSha256": hex.EncodeToString(sha256SumInvite(stage.Artifact)), "requestNotAfter": stage.RequestNotAfter}
		raw, err := adminCall(ctx, backend, "/v1/admin/invites/issue", "invite-issue", true, stage.OperationID, body)
		if err != nil {
			if reason.DefinitiveNoCommit(err) {
				if removeErr := handle.RemoveOwnerPrivate(stagePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					return s.handle(cmd, reason.New(reason.ReasonStorageFailure, "definitively rejected invite stage could not be cleared"))
				}
			}
			return s.handle(cmd, err)
		}
		if err := publishInviteIssue(stage, file, cmd.Int("invite-fd"), fdSet); err != nil {
			return s.handle(cmd, reason.New(reason.ReasonCredentialUnsafe, "invite artifact output could not be published"))
		}
		if err := handle.RemoveOwnerPrivate(stagePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return s.handle(cmd, reason.New(reason.ReasonStorageFailure, "invite issue stage could not be cleared"))
		}
		return writeAdminResult(s, cmd, "invite-issue", raw)
	}
}

func installationListAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		backend, err := remoteBackend(ctx, s, cmd)
		if err != nil {
			return err
		}
		defer backend.Close()
		raw, err := adminCall(ctx, backend, "/v1/admin/installations/list", "installation-list", false, strings.Repeat("0", 32), map[string]any{"includeRevoked": cmd.Bool("include-revoked")})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeAdminResult(s, cmd, "installation-list", raw)
	}
}

func installationRevokeAction(s *boundary) func(context.Context, *urfave.Command) error {
	return adminMutationAction(s, "installation-revoke", "/v1/admin/installations/revoke", func(cmd *urfave.Command) map[string]any {
		return map[string]any{"installationId": strings.TrimSpace(cmd.String("installation-id")), "reason": strings.TrimSpace(cmd.String("reason"))}
	})
}
func claimRevokeAction(s *boundary) func(context.Context, *urfave.Command) error {
	return adminMutationAction(s, "claim-revoke", "/v1/admin/claims/revoke", func(cmd *urfave.Command) map[string]any {
		return map[string]any{"claimId": strings.TrimSpace(cmd.String("claim-id")), "reason": strings.TrimSpace(cmd.String("reason"))}
	})
}
func adminMutationAction(s *boundary, operation, path string, fields func(*urfave.Command) map[string]any) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		backend, err := remoteBackend(ctx, s, cmd)
		if err != nil {
			return err
		}
		defer backend.Close()
		deadline, err := adminDeadline(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		body := fields(cmd)
		body["requestNotAfter"] = deadline
		raw, err := adminCall(ctx, backend, path, operation, true, strings.TrimSpace(cmd.String("operation-id")), body)
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeAdminResult(s, cmd, operation, raw)
	}
}

func recoveryStatusAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		backend, err := remoteBackend(ctx, s, cmd)
		if err != nil {
			return err
		}
		defer backend.Close()
		raw, err := adminCall(ctx, backend, "/v1/admin/recovery/status", "recovery-status", false, strings.Repeat("0", 32), map[string]any{})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeAdminResult(s, cmd, "recovery-status", raw)
	}
}

func remoteGCAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if !cmd.Bool("apply") {
			return s.handle(cmd, reason.Invalid("remote GC requires --apply"))
		}
		if strings.TrimSpace(cmd.String("cutoff")) != "" && cmd.IsSet("retention-days") {
			return s.handle(cmd, reason.Invalid("retention-days and cutoff are mutually exclusive"))
		}
		backend, err := remoteBackend(ctx, s, cmd)
		if err != nil {
			return err
		}
		defer backend.Close()
		deadline, err := adminDeadline(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		body := map[string]any{"cutoff": strings.TrimSpace(cmd.String("cutoff")), "retentionDays": cmd.Float64("retention-days"), "apply": true, "requestNotAfter": deadline}
		raw, err := adminCall(ctx, backend, "/v1/admin/gc", "gc", true, strings.TrimSpace(cmd.String("operation-id")), body)
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeAdminResult(s, cmd, "gc", raw)
	}
}

func recoveryReopenAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		path := strings.TrimSpace(cmd.String("attestation-file"))
		data, err := handle.ReadOwnerPrivate(path, 128*1024)
		if err != nil {
			return s.handle(cmd, reason.New(reason.ReasonCredentialUnsafe, "attestation file cannot be read safely"))
		}
		var attestation map[string]any
		if err := json.Unmarshal(data, &attestation); err != nil {
			return s.handle(cmd, reason.Invalid("attestation must be a JSON object"))
		}
		backend, err := remoteBackend(ctx, s, cmd)
		if err != nil {
			return err
		}
		defer backend.Close()
		deadline, err := adminDeadline(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		body := map[string]any{"expectedRecoveryRevision": cmd.Int64("expected-recovery-revision"), "attestation": attestation, "requestNotAfter": deadline}
		raw, err := adminCall(ctx, backend, "/v1/admin/recovery/reopen", "recovery-reopen", true, strings.TrimSpace(cmd.String("operation-id")), body)
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeAdminResult(s, cmd, "recovery-reopen", raw)
	}
}
