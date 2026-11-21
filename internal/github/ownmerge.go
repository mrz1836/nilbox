package github

import (
	"encoding/json"
	"fmt"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// ownMergeFieldsFragment is what the merge state query reads, with $own set, for a routine
// that merges the viewer's own pull request past the rules (FetchOwnMergeState): whether the
// viewer opened it, its review decision, the head commit's checks and statuses together
// (statusCheckRollup, the first hundred), its newest hundred commits' signatures and who
// signed each, its review threads (the first hundred), and the base branch's classic
// protection and the rules its rulesets apply there (the first fifty), with the checks a
// required-status-checks rule names. Every list is read with its count, so a list longer
// than its page is told from a whole one.
const ownMergeFieldsFragment = `
fragment OwnMergeFields on PullRequest {
  reviewDecision viewerDidAuthor
  statusCheckRollup {
    state
    contexts(first: 100) {
      totalCount
      nodes {
        __typename
        ... on CheckRun { name conclusion status }
        ... on StatusContext { context state }
      }
    }
  }
  ownCommits: commits(last: 100) {
    totalCount
    nodes { commit { oid signature { isValid signer { login } } } }
  }
  reviewThreads(first: 100) { totalCount nodes { isResolved } }
  baseRef {
    branchProtectionRule {
      requiredStatusCheckContexts requiresDeployments lockBranch requiresLinearHistory
    }
    rules(first: 50) {
      totalCount
      nodes {
        type
        parameters {
          __typename
          ... on RequiredStatusChecksParameters { requiredStatusChecks { context } }
        }
      }
    }
  }
}
`

// The rule types a ruleset applies to a branch (GitHub's RepositoryRuleType) that a
// routine's merge past the rules reads by name.
const (
	RuleCreation             = "CREATION"
	RuleDeletion             = "DELETION"
	RuleNonFastForward       = "NON_FAST_FORWARD"
	RulePullRequest          = "PULL_REQUEST"
	RuleRequiredStatusChecks = "REQUIRED_STATUS_CHECKS"
	RuleRequiredSignatures   = "REQUIRED_SIGNATURES"
	RuleRequiredLinear       = "REQUIRED_LINEAR_HISTORY"
	RuleCopilotCodeReview    = "COPILOT_CODE_REVIEW"
	RuleFilePathRestriction  = "FILE_PATH_RESTRICTION"
	RuleMaxFilePathLength    = "MAX_FILE_PATH_LENGTH"
	RuleFileExtension        = "FILE_EXTENSION_RESTRICTION"
	RuleMaxFileSize          = "MAX_FILE_SIZE"
)

// The rollup's states, as GitHub's StatusState spells them: a check run's conclusion and a
// status's state that passed are RollupSuccess too.
const (
	RollupSuccess  = "SUCCESS"
	RollupPending  = "PENDING"
	RollupExpected = "EXPECTED"
)

// The review decisions GitHub's reviewDecision reports.
const (
	ReviewDecisionApproved         = "APPROVED"
	ReviewDecisionChangesRequested = "CHANGES_REQUESTED"
	ReviewDecisionRequired         = "REVIEW_REQUIRED"
)

// OwnMerge is what FetchOwnMergeState read beyond the merge state: what a routine checks
// before it merges the viewer's own pull request past the rules. Each list keeps the count
// GitHub gave beside the page it read, so a list longer than its page reads as such.
type OwnMerge struct {
	// Viewer is the account the token belongs to, and ViewerDidAuthor reports that it
	// opened the pull request.
	Viewer          string
	ViewerDidAuthor bool

	// ReviewDecision is APPROVED, CHANGES_REQUESTED, or REVIEW_REQUIRED, or "" when the
	// branch needs no review.
	ReviewDecision string

	// Rollup is the head commit's checks and statuses together (SUCCESS, FAILURE, PENDING,
	// ERROR, or EXPECTED), "" when GitHub has none for it; Contexts are its checks and
	// statuses, the first hundred, and ContextCount how many there are.
	Rollup       string
	Contexts     []RollupContext
	ContextCount int

	// Commits are its newest commits, at most a hundred, each with its signature, and
	// CommitCount how many it has.
	Commits     []SignedCommit
	CommitCount int

	// ThreadCount counts its review threads, ThreadsRead those read (the first hundred),
	// and Unresolved those read that aren't resolved.
	ThreadCount int
	ThreadsRead int
	Unresolved  int

	// BaseRead reports a base branch GitHub returned, which a base deleted since isn't.
	// RulesRead reports its rulesets' rules read, Rules them, the first fifty, and RuleCount
	// how many there are. Protection is its classic branch protection, nil for none.
	BaseRead   bool
	RulesRead  bool
	Rules      []BranchRule
	RuleCount  int
	Protection *ClassicProtection
}

// unreadCount is the count of a list GitHub left null: one past the none read, so the list
// reads as not whole (ChecksWhole, CommitsWhole, ThreadsWhole, RulesWhole).
const unreadCount = 1

// ChecksWhole reports that every check and status of the rollup was read.
func (o *OwnMerge) ChecksWhole() bool { return o.ContextCount <= len(o.Contexts) }

// CommitsWhole reports that every commit of the pull request was read.
func (o *OwnMerge) CommitsWhole() bool { return o.CommitCount <= len(o.Commits) }

// ThreadsWhole reports that every review thread was read.
func (o *OwnMerge) ThreadsWhole() bool { return o.ThreadCount <= o.ThreadsRead }

// RulesWhole reports that every rule the base branch's rulesets apply was read, each with
// its parameters.
func (o *OwnMerge) RulesWhole() bool {
	if !o.BaseRead || !o.RulesRead || o.RuleCount > len(o.Rules) {
		return false
	}

	for _, r := range o.Rules {
		if !r.ParamsRead {
			return false
		}
	}

	return true
}

// RollupContext is one of the rollup's checks or statuses: a check run's name and its
// conclusion ("" while it runs), or a status's context and its state.
type RollupContext struct {
	Name  string
	State string
}

// Passed reports a check run that passed, or a status that did.
func (c RollupContext) Passed() bool {
	return c.State == RollupSuccess
}

// SignedCommit is one of a pull request's commits and its signature: Signed reports one
// it has, Valid that GitHub verified it, and Signer the account whose key signed it, ""
// when GitHub matched none.
type SignedCommit struct {
	OID    string
	Signed bool
	Valid  bool
	Signer string
}

// BranchRule is one rule a ruleset applies to the base branch: its type, and, for a
// required-status-checks rule, the checks it names (Checks), with ParamsRead reporting
// that its parameters were read as its type has them.
type BranchRule struct {
	Type       string
	Checks     []string
	ParamsRead bool
}

// ClassicProtection is the base branch's classic branch protection: the checks it
// requires, and whether it requires deployments, locks the branch, or wants a linear
// history.
type ClassicProtection struct {
	RequiredChecks      []string
	RequiresDeployments bool
	LockBranch          bool
	RequiresLinear      bool
}

// mergeStateData is the merge state query's answer: the pull request, and, with $own set,
// the viewer.
type mergeStateData struct {
	Node      json.RawMessage  `json:"node"`
	Viewer    *graphQLActor    `json:"viewer"`
	RateLimit graphQLRateLimit `json:"rateLimit"`
}

// mergeStateGone reports a merge state answer whose pull request GitHub could not find, as
// goneNode does an item's.
func mergeStateGone(r *graphQLResponse[mergeStateData]) (bool, error) {
	return goneNode(&graphQLResponse[itemData]{Data: &itemData{Node: r.Data.Node}, Errors: r.Errors})
}

// ownMergeNode is OwnMergeFields' part of the answer. GitHub's nulls decode to nil.
type ownMergeNode struct {
	ReviewDecision    string `json:"reviewDecision"`
	ViewerDidAuthor   bool   `json:"viewerDidAuthor"`
	StatusCheckRollup *struct {
		State    string `json:"state"`
		Contexts *struct {
			TotalCount int `json:"totalCount"`
			Nodes      []*struct {
				TypeName   string `json:"__typename"`
				Name       string `json:"name"`
				Conclusion string `json:"conclusion"`
				Context    string `json:"context"`
				State      string `json:"state"`
			} `json:"nodes"`
		} `json:"contexts"`
	} `json:"statusCheckRollup"`
	OwnCommits *struct {
		TotalCount int `json:"totalCount"`
		Nodes      []*struct {
			Commit *struct {
				OID       string `json:"oid"`
				Signature *struct {
					IsValid bool          `json:"isValid"`
					Signer  *graphQLActor `json:"signer"`
				} `json:"signature"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"ownCommits"`
	ReviewThreads *struct {
		TotalCount int `json:"totalCount"`
		Nodes      []*struct {
			IsResolved bool `json:"isResolved"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
	BaseRef *struct {
		BranchProtectionRule *struct {
			RequiredStatusCheckContexts []string `json:"requiredStatusCheckContexts"`
			RequiresDeployments         bool     `json:"requiresDeployments"`
			LockBranch                  bool     `json:"lockBranch"`
			RequiresLinearHistory       bool     `json:"requiresLinearHistory"`
		} `json:"branchProtectionRule"`
		Rules *struct {
			TotalCount int `json:"totalCount"`
			Nodes      []*struct {
				Type       string             `json:"type"`
				Parameters *graphQLRuleParams `json:"parameters"`
			} `json:"nodes"`
		} `json:"rules"`
	} `json:"baseRef"`
}

// decodeOwnMerge reads OwnMergeFields' part of the pull request node, and the viewer, as
// OwnMerge has them. A part GitHub left null reads as nothing read (its count past what was
// read, or the base or its rules not read), never as an empty one, so a check of it fails
// closed; no viewer is a malformed answer.
func decodeOwnMerge(raw json.RawMessage, viewer *graphQLActor) (*OwnMerge, error) {
	if viewer == nil || viewer.Login == "" {
		return nil, fmt.Errorf("%w: no viewer in the merge state", ErrMalformedResponse)
	}

	var n ownMergeNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, fmt.Errorf("%w: decode the own merge state: %w", ErrMalformedResponse, err)
	}

	out := &OwnMerge{
		Viewer: viewer.Login, ViewerDidAuthor: n.ViewerDidAuthor, ReviewDecision: n.ReviewDecision,
		Contexts: []RollupContext{}, Commits: []SignedCommit{}, Rules: []BranchRule{},
	}

	n.rollup(out)
	n.commits(out)
	n.threads(out)
	n.base(out)

	return out, nil
}

// rollup reads the head commit's checks into out: a null rollup is none, and a null
// context counts past those read.
func (n *ownMergeNode) rollup(out *OwnMerge) {
	r := n.StatusCheckRollup
	if r == nil {
		return
	}

	out.Rollup = r.State

	if r.Contexts == nil {
		out.ContextCount = unreadCount

		return
	}

	out.ContextCount = r.Contexts.TotalCount

	for _, c := range r.Contexts.Nodes {
		switch {
		case c == nil:
			out.ContextCount++
		case c.TypeName == "StatusContext":
			out.Contexts = append(out.Contexts, RollupContext{Name: c.Context, State: c.State})
		default:
			out.Contexts = append(out.Contexts, RollupContext{Name: c.Name, State: c.Conclusion})
		}
	}
}

// commits reads the newest commits' signatures into out: a null list, or a null commit,
// counts past those read.
func (n *ownMergeNode) commits(out *OwnMerge) {
	cs := n.OwnCommits
	if cs == nil {
		out.CommitCount = unreadCount

		return
	}

	out.CommitCount = cs.TotalCount

	for _, node := range cs.Nodes {
		if node == nil || node.Commit == nil {
			out.CommitCount++

			continue
		}

		c := SignedCommit{OID: node.Commit.OID}
		if s := node.Commit.Signature; s != nil {
			c.Signed, c.Valid = true, s.IsValid

			if s.Signer != nil {
				c.Signer = s.Signer.Login
			}
		}

		out.Commits = append(out.Commits, c)
	}
}

// threads counts the review threads into out: a null list, or a null thread, counts past
// those read.
func (n *ownMergeNode) threads(out *OwnMerge) {
	ts := n.ReviewThreads
	if ts == nil {
		out.ThreadCount = unreadCount

		return
	}

	out.ThreadCount = ts.TotalCount

	for _, t := range ts.Nodes {
		switch {
		case t == nil:
			out.ThreadCount++
		case !t.IsResolved:
			out.ThreadsRead++
			out.Unresolved++
		default:
			out.ThreadsRead++
		}
	}
}

// base reads the base branch's protection and rules into out: a null base, or null rules,
// is not read, and a null rule counts past those read.
func (n *ownMergeNode) base(out *OwnMerge) {
	b := n.BaseRef
	if b == nil {
		return
	}

	out.BaseRead = true

	if p := b.BranchProtectionRule; p != nil {
		out.Protection = &ClassicProtection{
			RequiredChecks: textutil.NonNil(p.RequiredStatusCheckContexts), RequiresDeployments: p.RequiresDeployments,
			LockBranch: p.LockBranch, RequiresLinear: p.RequiresLinearHistory,
		}
	}

	if b.Rules == nil {
		return
	}

	out.RulesRead, out.RuleCount = true, b.Rules.TotalCount

	for _, r := range b.Rules.Nodes {
		if r == nil {
			out.RuleCount++

			continue
		}

		out.Rules = append(out.Rules, ruleOf(r.Type, r.Parameters))
	}
}

// graphQLRuleParams is a rule's parameters, as OwnMergeFields reads them: their type, and a
// required-status-checks rule's checks.
type graphQLRuleParams struct {
	TypeName             string `json:"__typename"`
	RequiredStatusChecks []*struct {
		Context string `json:"context"`
	} `json:"requiredStatusChecks"`
}

// ruleOf is one rule as BranchRule has it: a required-status-checks rule's parameters are
// read only when GitHub gave them as that rule's, each check with its context.
func ruleOf(kind string, params *graphQLRuleParams) BranchRule {
	out := BranchRule{Type: kind, Checks: []string{}, ParamsRead: kind != RuleRequiredStatusChecks}

	if kind != RuleRequiredStatusChecks || params == nil || params.TypeName != "RequiredStatusChecksParameters" {
		return out
	}

	out.ParamsRead = true

	for _, c := range params.RequiredStatusChecks {
		if c == nil || c.Context == "" {
			out.ParamsRead = false

			continue
		}

		out.Checks = append(out.Checks, c.Context)
	}

	return out
}
