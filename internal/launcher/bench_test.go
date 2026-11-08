package launcher

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/trust"
)

// benchWorld is testWorld with launch defaults for the item's org and its repository,
// so resolution walks every layer: the org picks the profile and plan mode, and the
// repository the model, each adding extra arguments.
func benchWorld() world {
	name := prSubject().repo

	w := testWorld()
	w.policy = trust.NewPolicy(trust.DefaultBots())
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: name.Owner, Profiles: claudeOnly(profileWork), PlanMode: new(true), ExtraArgs: claudeOnly([]string{"--org"})},
		{Owner: name.Owner, Name: name.Repo, Models: claudeOnly(strSonnet), ExtraArgs: claudeOnly([]string{"--repo"})},
	})

	return w
}

// benchRow is baseRequest's pull request, opened by a member of its org, as a launch
// loads it.
func benchRow() launchItemRow {
	it, name := baseRequest().Item, prSubject().repo

	return launchItemRow{StoredItem: StoredItem{
		ID: 7, Kind: it.Kind, Number: it.Number, URL: it.URL, Title: it.Title, HeadRef: it.Branch,
		AuthorLogin: "octo-user", AuthorType: "User", AuthorAssociation: trust.AssociationMember,
		RepoID: 1, Owner: name.Owner, Name: name.Repo,
	}}
}

// BenchmarkResolveAndPlan works out one pull request's launch from loaded state: it
// judges the item's trust, resolves every launch setting through the launch, the prompt,
// the repository's and the org's defaults, the profile, and the app's settings, and
// builds the plan from them. That is the pure part of every Preview and Launch, the
// work between reading the settings and the item and touching the terminal, the clone,
// the agent binary, or GitHub, which the benchmark stands in for with a fixed clone and
// binary path.
func BenchmarkResolveAndPlan(b *testing.B) {
	w, row, base := benchWorld(), benchRow(), baseRequest()
	req := LaunchRequest{ItemID: row.ID}

	plan := func() (Plan, error) {
		res, profile, prompt, err := resolve(w, row.subject(w.policy), req)
		if err != nil {
			return Plan{}, err
		}

		return BuildPlan(Request{
			Item: row.item(w.policy), Agent: res.Agent.ID, Profile: effectiveProfile(profile, res), Dir: base.Dir,
			BinaryPath: base.BinaryPath, SessionName: sessionName(prompt.Nickname, row),
		})
	}

	p, err := plan()
	require.NoError(b, err)
	require.Equal(b, base.Dir, p.Dir)

	b.ReportAllocs()

	for b.Loop() {
		if _, err = plan(); err != nil {
			b.Fatal(err)
		}
	}
}
