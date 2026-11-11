package github

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/mrz1836/nilbox/internal/repo"
)

// batchAlias is the alias of the i-th field of a batched query: prefix, then i, as in r0.
// The query asks under it, and the answer is read back under the same alias.
func batchAlias(prefix string, i int) string {
	return prefix + strconv.Itoa(i)
}

// repoField is the field that asks about repository r under alias in a batched query, up
// to its selection, as in r0: repository(owner: "acme-org", name: "example-repo"). The
// owner and name are written with graphQLString, so neither can break out of its argument.
func repoField(alias string, r repo.Name) (string, error) {
	owner, err := graphQLString(r.Owner)
	if err != nil {
		return "", err
	}

	name, err := graphQLString(r.Repo)
	if err != nil {
		return "", err
	}

	return alias + ": repository(owner: " + owner + ", name: " + name + ")", nil
}

// graphQLString writes s as a JSON string, which GraphQL reads as the same string, with
// every quote, backslash, and control character escaped.
func graphQLString(s string) (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("encode %q for a query: %w", s, err)
	}

	return string(raw), nil
}
