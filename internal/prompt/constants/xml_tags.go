package constants

// XML tag constants ported from constants/xml.ts.
// [P1.T2] TS anchor: constants/xml.ts

// ── Command metadata tags ──────────────────────────────────────────────────
const (
	CommandNameTag    = "command-name"
	CommandMessageTag = "command-message"
	CommandArgsTag    = "command-args"
)

// ── Terminal / bash content tags ────────────────────────────────────────────
const (
	BashInputTag           = "bash-input"
	BashStdoutTag          = "bash-stdout"
	BashStderrTag          = "bash-stderr"
	LocalCommandStdoutTag  = "local-command-stdout"
	LocalCommandStderrTag  = "local-command-stderr"
	LocalCommandCaveatTag  = "local-command-caveat"
)

// TerminalOutputTags lists all tags that indicate terminal output (not a user prompt).
var TerminalOutputTags = []string{
	BashInputTag,
	BashStdoutTag,
	BashStderrTag,
	LocalCommandStdoutTag,
	LocalCommandStderrTag,
	LocalCommandCaveatTag,
}

// TickTag is the XML tag for periodic tick messages (proactive mode).
const TickTag = "tick"

// ── Task notification tags ─────────────────────────────────────────────────
const (
	TaskNotificationTag = "task-notification"
	TaskIDTag           = "task-id"
	ToolUseIDTag        = "tool-use-id"
	TaskTypeTag         = "task-type"
	OutputFileTag       = "output-file"
	StatusTag           = "status"
	SummaryTag          = "summary"
	ReasonTag           = "reason"
	WorktreeTag         = "worktree"
	WorktreePathTag     = "worktreePath"
	WorktreeBranchTag   = "worktreeBranch"
)

// UltraplanTag is used for remote parallel planning sessions.
const UltraplanTag = "ultraplan"

// RemoteReviewTag wraps the final review from a remote /review session.
const RemoteReviewTag = "remote-review"

// RemoteReviewProgressTag wraps run_hunt.sh heartbeat progress.
const RemoteReviewProgressTag = "remote-review-progress"

// ── Swarm / inter-agent tags ───────────────────────────────────────────────
const (
	TeammateMessageTag     = "teammate-message"
	ChannelMessageTag      = "channel-message"
	ChannelTag             = "channel"
	CrossSessionMessageTag = "cross-session-message"
)

// ── Fork boilerplate ───────────────────────────────────────────────────────
const (
	ForkBoilerplateTag  = "fork-boilerplate"
	ForkDirectivePrefix = "Your directive: "
)

// ── Common slash-command argument patterns ──────────────────────────────────
var CommonHelpArgs = []string{"help", "-h", "--help"}

var CommonInfoArgs = []string{
	"list", "show", "display", "current", "view",
	"get", "check", "describe", "print", "version",
	"about", "status", "?",
}
