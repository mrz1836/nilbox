package core

import (
	"context"

	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/people"
	"github.com/mrz1836/nilbox/internal/repo"
)

// assigneesDirectory hands the assignee Finder, which both sheets share, the People
// directory: who can be assigned in each repository, who works on it and on the owner's
// other repositories, and the reads of GitHub's assignable users it keeps.
type assigneesDirectory struct {
	directory *people.Service
}

// Known returns what the directory knows of who can be assigned in the repository name.
// The owner's other repositories are read only while GitHub's whole list is cut short: a
// list that named everyone leaves them out.
func (a assigneesDirectory) Known(ctx context.Context, name repo.Name) (assignees.Known, error) {
	got, err := a.directory.Assignable(ctx, name)
	if err != nil {
		return assignees.Known{}, err
	}

	ranked, err := a.directory.Ranked(ctx, name)
	if err != nil {
		return assignees.Known{}, err
	}

	out := assignees.Known{
		Assignable: make([]assignees.Assignable, 0, len(got.People)), WorksHere: workersOf(ranked.People),
		Org: []assignees.Worker{}, Total: got.Total, Complete: got.Complete, ReadAt: got.ReadAt, Stale: got.Stale,
	}

	for _, p := range got.People {
		out.Assignable = append(out.Assignable, assignees.Assignable(p))
	}

	if got.Complete {
		return out, nil
	}

	org, err := a.directory.OrgRanked(ctx, name)
	if err != nil {
		return assignees.Known{}, err
	}

	out.Org = workersOf(org)

	return out, nil
}

// Covers reports that what the directory holds answers a search of the repository name for
// query.
func (a assigneesDirectory) Covers(ctx context.Context, name repo.Name, query string) (bool, error) {
	return a.directory.Covers(ctx, name, query)
}

// Keep keeps what a read of the repository name's assignable users for query found.
func (a assigneesDirectory) Keep(ctx context.Context, name repo.Name, query string, found assignees.Found) error {
	out := make([]people.Found, 0, len(found.People))
	for _, p := range found.People {
		out = append(out, people.Found(p))
	}

	return a.directory.KeepAssignable(ctx, name, query, out, found.Total)
}

// MarkFailed records a read of the repository name's assignable users for query that failed.
func (a assigneesDirectory) MarkFailed(ctx context.Context, name repo.Name, query string) error {
	return a.directory.MarkAssignableFailed(ctx, name, query)
}

// Assigned keeps logins as assigned in the repository name.
func (a assigneesDirectory) Assigned(ctx context.Context, name repo.Name, logins []string) error {
	return a.directory.SeenAssigned(ctx, name, logins)
}

// workersOf converts the people who work on a repository, as the directory ranks them.
func workersOf(list []people.Person) []assignees.Worker {
	out := make([]assignees.Worker, 0, len(list))
	for _, p := range list {
		out = append(out, assignees.Worker{
			Login: p.Login, Name: p.Name, Reviewed: p.Reviewed, Authored: p.Authored, Committed: p.Committed,
			LastAt: p.LastAt, Where: p.Where,
		})
	}

	return out
}
