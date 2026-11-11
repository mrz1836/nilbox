package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/repo"
)

// errNotUTF8 is returned for a value that is not valid UTF-8, which no query asks about:
// written as JSON, its bad bytes would become U+FFFD, and the query would ask about some
// other value.
var errNotUTF8 = errors.New("not valid UTF-8")

// batchAlias is the alias of the i-th field of a batched query: prefix, then i, as in r0.
// Each query names its prefix once, in a constant that both the query and the reading of
// its answer use.
func batchAlias(prefix string, i int) string {
	return prefix + strconv.Itoa(i)
}

// repoField is the field that asks about repository r under alias in a batched query, up
// to its selection, as in r0: repository(owner: "acme-org", name: "example-repo"). The
// owner and name are written with graphQLString, so neither can break out of its argument.
func repoField(alias string, r repo.Name) (string, error) {
	owner, err := graphQLString(r.Owner)
	if err != nil {
		return "", fmt.Errorf("encode the repository's owner: %w", err)
	}

	name, err := graphQLString(r.Repo)
	if err != nil {
		return "", fmt.Errorf("encode the repository's name: %w", err)
	}

	return alias + ": repository(owner: " + owner + ", name: " + name + ")", nil
}

// graphQLString writes s as a JSON string, which GraphQL reads as the same string. Its
// quotes, backslashes, and C0 control characters (U+0000 to U+001F, line breaks among
// them) are escaped, and so are <, >, &, U+2028, and U+2029, which JSON escapes too; DEL
// and the C1 control characters, which a GraphQL string may hold, are written as they
// are. A value that is not valid UTF-8 is refused with errNotUTF8, never rewritten.
func graphQLString(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("%q: %w", s, errNotUTF8)
	}

	raw, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("%q: %w", s, err)
	}

	return string(raw), nil
}
