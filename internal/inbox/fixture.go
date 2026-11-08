package inbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

// Fixture is an inbox to store as it is, without asking GitHub, for seeding measurement
// and test databases (see StoreFixture): the account the items are stored for, and the
// items with the lists each is in.
type Fixture struct {
	// Viewer is the login of the account the items are stored for.
	Viewer string

	// Items are stored in order, each with its repository.
	Items []FixtureItem

	// At stamps what is stored, as a refresh at that time would; zero is the current
	// time.
	At time.Time
}

// FixtureItem is one item of a Fixture: what GitHub would say about it, the lists it is
// in, and its details. An item in no list is stored but not listed, as a refresh leaves
// an item no search returns any more.
type FixtureItem struct {
	github.Item

	ReviewRequested bool
	Authored        bool
	Assigned        bool

	// Details, when set, are stored as the item's details, as if a refresh had read
	// them.
	Details *github.ItemDetails
}

// StoreFixture stores f in db, in one transaction, the way a refresh stores what it read:
// the account, each item's repository, and the items with their lists and details. It
// writes no inbox status, rate reading, or file check, and leaves alone every item that
// f does not name. It returns the stored items' ids in the order of f.Items.
//
// It is for seeding measurement and test databases. The app itself stores only what a
// refresh reads from GitHub.
func StoreFixture(ctx context.Context, db *sql.DB, f Fixture) ([]int64, error) {
	at := f.At
	if at.IsZero() {
		at = time.Now()
	}

	ids := make([]int64, 0, len(f.Items))

	err := store.InTx(ctx, db, func(tx *sql.Tx) error {
		w := newWriter(queries.New(tx), nil, at.UTC())

		if err := w.saveAccount(ctx, f.Viewer); err != nil {
			return err
		}

		for _, fi := range f.Items {
			id, err := w.saveFixtureItem(ctx, fi)
			if err != nil {
				return err
			}

			ids = append(ids, id)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store the fixture: %w", err)
	}

	return ids, nil
}

// saveFixtureItem stores one fixture item with its repository and details, and returns
// its id.
func (w *writer) saveFixtureItem(ctx context.Context, fi FixtureItem) (int64, error) {
	repoID, err := w.repoID(ctx, fi.Item)
	if err != nil {
		return 0, err
	}

	w.details = nil
	if fi.Details != nil {
		w.details = map[string]github.ItemDetails{fi.NodeID: *fi.Details}
	}

	e := &entry{item: fi.Item, reviewRequested: fi.ReviewRequested, authored: fi.Authored, assigned: fi.Assigned}
	if err = w.saveItem(ctx, repoID, e); err != nil {
		return 0, err
	}

	return w.saved[len(w.saved)-1].id, nil
}
