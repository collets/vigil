package cli

import (
	"errors"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"vigil/internal/core"
	"vigil/internal/spike"
	"vigil/internal/store"
)

func addFinalizationCommands(root *cobra.Command, stateDir *string) {
	var commitFile string
	prepareCommit := &cobra.Command{Use: "commit-prepare PROJECT_ID", Short: "Prepare an exact-tree task commit approval without touching the checkout", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if commitFile == "" {
			return errors.New("--file required")
		}
		file, err := os.Open(commitFile)
		if err != nil {
			return err
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, store.MaxDocument+1))
		if err != nil {
			return err
		}
		var request core.CommitRequest
		if err := store.Decode(content, &request); err != nil {
			return err
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.PrepareCommit(cmd.Context(), request)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	prepareCommit.Flags().StringVar(&commitFile, "file", "", "Exact bounded commit request JSON")
	root.AddCommand(prepareCommit)
	var commitGrant string
	executeCommit := &cobra.Command{Use: "commit-execute PROJECT_ID OPERATION_ID", Short: "Execute or reconcile one approved exact-tree task commit", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ExecuteCommit(cmd.Context(), args[1], commitGrant)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	executeCommit.Flags().StringVar(&commitGrant, "grant-id", "", "Exact human grant for a prepared operation; omitted only when reconciling an already started operation")
	root.AddCommand(executeCommit)
	var pushCommand, pushRemote, pushCredential string
	preparePush := &cobra.Command{Use: "push-prepare PROJECT_ID PLAN_ID REPOSITORY_ID", Short: "Inspect and request authority for one exact plan ref push", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		if pushCommand == "" || pushRemote == "" {
			return errors.New("--command-id and --remote required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.PreparePush(cmd.Context(), core.PushRequest{CommandID: pushCommand, PlanID: args[1], RepositoryID: args[2], RemoteName: pushRemote, CredentialRef: pushCredential})
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	preparePush.Flags().StringVar(&pushCommand, "command-id", "", "Unique replay-safe push approval request command")
	preparePush.Flags().StringVar(&pushRemote, "remote", "", "Exact enrolled Git remote name")
	preparePush.Flags().StringVar(&pushCredential, "credential-ref", "", "Named SSH agent socket reference env:SSH_AUTH_SOCK for SSH remotes")
	root.AddCommand(preparePush)
	var pushGrant string
	executePush := &cobra.Command{Use: "push-execute PROJECT_ID OPERATION_ID", Short: "Execute or reconcile one approved non-force plan ref push", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ExecutePush(cmd.Context(), args[1], pushGrant)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	executePush.Flags().StringVar(&pushGrant, "grant-id", "", "Exact human grant for a prepared push; omit only for already started reconciliation")
	root.AddCommand(executePush)
	var draftFile string
	prepareDraft := &cobra.Command{Use: "draft-prepare PROJECT_ID", Short: "Prepare a separately authorized exact-head draft GitHub PR or GitLab MR", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if draftFile == "" {
			return errors.New("--file required")
		}
		file, err := os.Open(draftFile)
		if err != nil {
			return err
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, store.MaxDocument+1))
		if err != nil {
			return err
		}
		var request core.DraftRequest
		if err := store.Decode(content, &request); err != nil {
			return err
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.PrepareDraft(cmd.Context(), request)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	prepareDraft.Flags().StringVar(&draftFile, "file", "", "Bounded JSON with provider/project/base/title/body and a named credential reference")
	root.AddCommand(prepareDraft)
	var draftGrant string
	executeDraft := &cobra.Command{Use: "draft-execute PROJECT_ID OPERATION_ID", Short: "Create or reconcile one approved draft hosting request", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ExecuteDraft(cmd.Context(), args[1], draftGrant)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	executeDraft.Flags().StringVar(&draftGrant, "grant-id", "", "Exact human grant for a prepared draft; omit only for already started reconciliation")
	root.AddCommand(executeDraft)
	status := &cobra.Command{Use: "delivery-status PROJECT_ID OPERATION_ID", Short: "Inspect an exact delivery operation and any observed effect", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.DeliveryStatus(cmd.Context(), args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	root.AddCommand(status)
	var cancelCommand string
	cancelDelivery := &cobra.Command{Use: "delivery-cancel PROJECT_ID OPERATION_ID", Short: "Cancel a never-started delivery approval without an external effect", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if cancelCommand == "" {
			return errors.New("--command-id required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.CancelPreparedDelivery(cmd.Context(), cancelCommand, args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	cancelDelivery.Flags().StringVar(&cancelCommand, "command-id", "", "Unique human cancellation command for a prepared operation")
	root.AddCommand(cancelDelivery)
	var reconcileCommand string
	reconcileDelivery := &cobra.Command{Use: "delivery-reconcile PROJECT_ID OPERATION_ID", Short: "Close a stuck executing or uncertain delivery from a fresh exact observation", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if reconcileCommand == "" {
			return errors.New("--command-id required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ReconcileDelivery(cmd.Context(), reconcileCommand, args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	reconcileDelivery.Flags().StringVar(&reconcileCommand, "command-id", "", "Unique human reconciliation command for an executing or uncertain operation")
	root.AddCommand(reconcileDelivery)
	var closeCommand, closeAttestation string
	closeUnobserved := &cobra.Command{Use: "delivery-close-unobserved PROJECT_ID OPERATION_ID", Short: "Close a delivery an operator has verified externally did not take effect", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if closeCommand == "" {
			return errors.New("--command-id required")
		}
		if closeAttestation == "" {
			return errors.New("--attest required: describe the external state you verified")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.CloseUnobservedDelivery(cmd.Context(), closeCommand, args[1], closeAttestation)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	closeUnobserved.Flags().StringVar(&closeCommand, "command-id", "", "Unique human attestation command")
	closeUnobserved.Flags().StringVar(&closeAttestation, "attest", "", "Required: what external state you verified and why no effect occurred")
	root.AddCommand(closeUnobserved)

	var archiveCommand string
	build := &cobra.Command{Use: "archive-build PROJECT_ID PLAN_ID", Short: "Persist a verified factual archive before any optional narrative", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if archiveCommand == "" {
			return errors.New("--command-id required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			record, err := e.BuildFactualArchive(cmd.Context(), archiveCommand, args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, record)
		})
	}}
	build.Flags().StringVar(&archiveCommand, "command-id", "", "Unique replay-safe factual archive command")
	root.AddCommand(build)

	show := &cobra.Command{Use: "archive-show PROJECT_ID PLAN_ID REVISION", Short: "Verify and inspect an exact factual archive revision", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := strconv.Atoi(args[2])
		if err != nil || revision < 1 {
			return errors.New("REVISION must be positive")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			record, manifest, err := e.Archive(cmd.Context(), args[1], revision)
			if err != nil {
				return err
			}
			return printJSON(cmd, map[string]any{"record": record, "manifest": manifest})
		})
	}}
	root.AddCommand(show)

	var narrativeFile string
	var fixture bool
	narrative := &cobra.Command{Use: "archive-narrative PROJECT_ID PLAN_ID", Short: "Record one fixture finalization result with exact manifest citations", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !fixture || narrativeFile == "" {
			return errors.New("--synthetic-fixture and --file required; production narrative dispatch is disabled")
		}
		file, err := os.Open(narrativeFile)
		if err != nil {
			return err
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, store.MaxDocument+1))
		if err != nil {
			return err
		}
		var request core.NarrativeResult
		if err := store.Decode(content, &request); err != nil {
			return err
		}
		if request.PlanID != args[1] {
			return errors.New("narrative plan does not match command target")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			record, err := e.RecordFixtureNarrative(cmd.Context(), request)
			if err != nil {
				return err
			}
			return printJSON(cmd, record)
		})
	}}
	narrative.Flags().StringVar(&narrativeFile, "file", "", "Closed structured narrative-result JSON, at most 64 KiB")
	narrative.Flags().BoolVar(&fixture, "synthetic-fixture", false, "Label this as disposable fixture finalization")
	root.AddCommand(narrative)
	var finalizationManifest, finalizationKeyFile, finalizationCommand, finalizationProfile, finalizationDigest string
	var finalizationRevision, finalizationProfileRevision int
	var finalizationActiveMS int64
	var finalizationLiveLocal bool
	finalizationRun := &cobra.Command{Use: "finalization-run PROJECT_ID PLAN_ID", Short: "Run one bounded disposable-fixture narrative turn through local Hermes", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !finalizationLiveLocal {
			return errors.New("--live-local required; finalization inference is never implicit")
		}
		if finalizationManifest == "" || finalizationCommand == "" || finalizationProfile == "" || finalizationRevision < 1 || finalizationProfileRevision < 1 || finalizationDigest == "" || finalizationActiveMS < 1 || finalizationActiveMS > core.MaxPlanningAttempt.Milliseconds() {
			return errors.New("manifest, exact archive/profile identities and active-limit-ms (1..300000) required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			// The adapter is constructed from the persisted profile's own
			// route identity, never from operator-supplied route names.
			version, endpoint, credential, err := e.ProfileRoute(cmd.Context(), finalizationProfile, finalizationProfileRevision)
			if err != nil {
				return err
			}
			provider, err := spike.NewPlanningProvider(cmd.Context(), finalizationManifest, finalizationKeyFile,
				spike.Route{Version: version, EndpointID: endpoint, CredentialRef: credential})
			if err != nil {
				return err
			}
			record, err := e.RunFinalization(cmd.Context(), core.FinalizationRunRequest{CommandID: finalizationCommand, PlanID: args[1],
				ManifestRevision: finalizationRevision, ManifestDigest: finalizationDigest, ProfileID: finalizationProfile,
				ProfileRevision: finalizationProfileRevision, ActiveLimit: time.Duration(finalizationActiveMS) * time.Millisecond}, provider)
			if err != nil {
				return err
			}
			return printJSON(cmd, record)
		})
	}}
	finalizationRun.Flags().BoolVar(&finalizationLiveLocal, "live-local", false, "Authorize one turn through the prepared local llama/Hermes route")
	finalizationRun.Flags().StringVar(&finalizationManifest, "manifest", "", "Prepared local Hermes qualification manifest")
	finalizationRun.Flags().StringVar(&finalizationKeyFile, "key-file", "", "Optional private local llama key file (mode 600)")
	finalizationRun.Flags().StringVar(&finalizationCommand, "command-id", "", "Unique single-use finalization attempt command")
	finalizationRun.Flags().IntVar(&finalizationRevision, "archive-revision", 0, "Exact latest pending factual archive revision")
	finalizationRun.Flags().StringVar(&finalizationDigest, "manifest-digest", "", "Exact factual archive SHA-256 digest")
	finalizationRun.Flags().StringVar(&finalizationProfile, "profile-id", "", "Explicit eligible finalization profile")
	finalizationRun.Flags().IntVar(&finalizationProfileRevision, "profile-revision", 0, "Exact current finalization profile revision")
	finalizationRun.Flags().Int64Var(&finalizationActiveMS, "active-limit-ms", core.MaxPlanningAttempt.Milliseconds(), "Active finalization cap, maximum 300000ms")
	root.AddCommand(finalizationRun)
	quarantine := &cobra.Command{Use: "finalization-quarantine PROJECT_ID ATTEMPT_COMMAND_ID", Short: "Conservatively account for an overdue, unconfirmed fixture finalization attempt", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			state, err := e.QuarantineFinalizationAttempt(cmd.Context(), args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, map[string]string{"attempt_command_id": args[1], "state": state, "writer_state": "unconfirmed"})
		})
	}}
	root.AddCommand(quarantine)

	export := &cobra.Command{Use: "archive-export PROJECT_ID PLAN_ID REVISION DESTINATION", Short: "Copy verified archive records into a new portable private directory", Args: cobra.ExactArgs(4), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := strconv.Atoi(args[2])
		if err != nil || revision < 1 {
			return errors.New("REVISION must be positive")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ExportArchive(cmd.Context(), args[1], revision, args[3])
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	root.AddCommand(export)

	var retentionInspectCommand string
	inspect := &cobra.Command{Use: "retention-inspect PROJECT_ID", Short: "Dry-inspect completed-plan transcript expiry candidates as a durable receipt", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if retentionInspectCommand == "" {
			return errors.New("--command-id required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			candidates, err := e.InspectTranscriptExpiry(cmd.Context(), retentionInspectCommand, store.Now())
			if err != nil {
				return err
			}
			return printJSON(cmd, candidates)
		})
	}}
	inspect.Flags().StringVar(&retentionInspectCommand, "command-id", "", "Unique dry-run inspection command whose receipt expiry must consume")
	root.AddCommand(inspect)
	var retentionCommand, retentionInspectReceipt string
	expire := &cobra.Command{Use: "retention-expire PROJECT_ID", Short: "Expire eligible raw transcripts after the configured completed-plan interval", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if retentionCommand == "" {
			return errors.New("--command-id required")
		}
		if retentionInspectReceipt == "" {
			return errors.New("--inspect-command-id required: expiry consumes an unchanged dry-run inspection receipt")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ExpireTranscripts(cmd.Context(), retentionCommand, retentionInspectReceipt, store.Now())
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	expire.Flags().StringVar(&retentionCommand, "command-id", "", "Unique replay-safe transcript expiry command")
	expire.Flags().StringVar(&retentionInspectReceipt, "inspect-command-id", "", "Exact dry-run inspection command ID whose candidate set must be unchanged")
	root.AddCommand(expire)
}
