package regixtry

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// syntheticCatalog serves a fixed sorted list of names through the two
// listing methods the catalog uses, applying limit and cursor like the SQL
// does (limit before any access filtering).
type syntheticCatalog struct {
	ports.MetadataStore
	names   []string
	calls   int
	onFetch func(limit int)
}

func (c *syntheticCatalog) window(limit int, after string) []string {
	c.calls++
	if c.onFetch != nil {
		c.onFetch(limit)
	}
	out := []string{}
	for _, name := range c.names {
		if after != "" && name <= after {
			continue
		}
		out = append(out, name)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

func (c *syntheticCatalog) Catalog(_ context.Context, _ string, limit int, after string) ([]domain.RepositoryRef, error) {
	refs := []domain.RepositoryRef{}
	for _, name := range c.window(limit, after) {
		refs = append(refs, domain.MustParseRepositoryRef(name))
	}
	return refs, nil
}

func (c *syntheticCatalog) ListRepositoriesWithSummary(_ context.Context, _ string, limit int, after string) ([]ports.RepositorySummary, error) {
	out := []ports.RepositorySummary{}
	for _, name := range c.window(limit, after) {
		out = append(out, ports.RepositorySummary{Name: name, TagCount: 1})
	}
	return out, nil
}

func catalogServiceWith(t *testing.T, names []string) (*Service, *syntheticCatalog) {
	t.Helper()
	return catalogServiceWithController(t, names, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}))
}

func catalogServiceWithController(t *testing.T, names []string, controller ports.AccessController) (*Service, *syntheticCatalog) {
	t.Helper()
	service, cleanup := newTestService(t, controller)
	t.Cleanup(cleanup)
	sort.Strings(names)
	fake := &syntheticCatalog{MetadataStore: service.metadata, names: names}
	service.metadata = fake
	return service, fake
}

func catalogScope() []domainauth.Scope {
	scopes := []domainauth.Scope{{Type: "regixtry", Name: "catalog", Actions: []string{"*"}, Canonical: "registry:catalog:*"}}
	return scopes
}

func manyRepositories() []string {
	names := make([]string, 0, 520)
	for i := 0; i < 500; i++ {
		names = append(names, fmt.Sprintf("bulk/repo%04d", i))
	}
	for _, n := range []string{"odabank/a", "odabank/b/c", "odabank/d", "odabank2/x", "odabank", "zeta/odabank", "zzz/last"} {
		names = append(names, n)
	}
	return names
}

func pageThrough(t *testing.T, service *Service, ctx context.Context, limit int) []string {
	t.Helper()
	var all []string
	after := ""
	for i := 0; i < 1000; i++ {
		page, err := service.Catalog(ctx, limit, after)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Repositories) == 0 {
			return all
		}
		if len(page.Repositories) > limit {
			t.Fatalf("page of %d exceeds limit %d", len(page.Repositories), limit)
		}
		all = append(all, page.Repositories...)
		after = page.Repositories[len(page.Repositories)-1]
	}
	t.Fatal("paging did not terminate")
	return nil
}

// TestCatalogFillsPagesForAProjectAmongManyRepositories pins S6 (matching and
// catalog): a small project among 500 repositories returns full pages with no
// gaps or duplicates, and similar names are never included.
func TestCatalogFillsPagesForAProjectAmongManyRepositories(t *testing.T) {
	t.Parallel()
	service, fake := catalogServiceWith(t, manyRepositories())
	ctx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{
		ProjectGrants: []domainauth.ProjectGrant{{Project: "odabank", Role: domainauth.RepoRoleReader}},
		Scopes:        catalogScope(),
	})

	first, err := service.Catalog(ctx, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(first.Repositories, ",") != "odabank/a,odabank/b/c" {
		t.Fatalf("first page = %v, want a full page of project repositories", first.Repositories)
	}
	if fake.calls > 10 {
		t.Fatalf("store queried %d times for one page", fake.calls)
	}

	for _, limit := range []int{1, 2, 3, 100, 1000} {
		got := pageThrough(t, service, ctx, limit)
		if strings.Join(got, ",") != "odabank/a,odabank/b/c,odabank/d" {
			t.Fatalf("limit %d: paged = %v, want exactly the project repositories, once each", limit, got)
		}
	}

	summaries, err := service.RepositorySummaries(ctx, 2, "")
	if err != nil || len(summaries) != 2 || summaries[0].Name != "odabank/a" || summaries[1].Name != "odabank/b/c" {
		t.Fatalf("RepositorySummaries() = %#v, %v, want a full page", summaries, err)
	}
	summaries, err = service.RepositorySummaries(ctx, 2, "odabank/b/c")
	if err != nil || len(summaries) != 1 || summaries[0].Name != "odabank/d" {
		t.Fatalf("RepositorySummaries(after) = %#v, %v", summaries, err)
	}
}

// TestCatalogMatchesBaselineForExactGrants compares the new paging against
// the previous behavior (one fetch, then filter) on every case where the
// previous behavior was already complete: unlimited requests, a limit that
// covers the store, a caller with no filtering, and the union of all pages.
func TestCatalogMatchesBaselineForExactGrants(t *testing.T) {
	t.Parallel()
	names := manyRepositories()
	service, _ := catalogServiceWith(t, names)
	exact := domainauth.Principal{
		Grants: []domainauth.RepoGrant{
			{Repository: domain.MustParseRepositoryRef("bulk/repo0007"), Role: domainauth.RepoRoleReader},
			{Repository: domain.MustParseRepositoryRef("odabank"), Role: domainauth.RepoRoleWriter},
			{Repository: domain.MustParseRepositoryRef("zzz/last"), Role: domainauth.RepoRoleAdmin},
		},
		Scopes: catalogScope(),
	}
	ctx := ports.ContextWithPrincipal(context.Background(), exact)

	baseline := func(limit int, after string) []string {
		fetched := []string{}
		for _, name := range names {
			if after != "" && name <= after {
				continue
			}
			fetched = append(fetched, name)
			if limit > 0 && len(fetched) == limit {
				break
			}
		}
		out := []string{}
		for _, name := range fetched {
			if exact.HasCatalogAccess(name) {
				out = append(out, name)
			}
		}
		return out
	}

	for _, limit := range []int{0, 600, 10000} {
		got, err := service.Catalog(ctx, limit, "")
		if err != nil {
			t.Fatal(err)
		}
		if want := baseline(limit, ""); strings.Join(got.Repositories, ",") != strings.Join(want, ",") {
			t.Fatalf("limit %d: got %v, baseline %v", limit, got.Repositories, want)
		}
	}
	if got := pageThrough(t, service, ctx, 7); strings.Join(got, ",") != "bulk/repo0007,odabank,zzz/last" {
		t.Fatalf("paged exact grants = %v", got)
	}

	// No principal (local trust) and a global admin are not filtered: same
	// single fetch as before.
	local, localFake := catalogServiceWithController(t, names, allowAllAccessController{})
	admin, adminFake := catalogServiceWith(t, names)
	for _, c := range []struct {
		service *Service
		fake    *syntheticCatalog
		ctx     context.Context
	}{
		{local, localFake, context.Background()},
		{admin, adminFake, ports.ContextWithPrincipal(context.Background(), domainauth.Principal{IsAdmin: true, Scopes: catalogScope()})},
	} {
		c.fake.calls = 0
		page, err := c.service.Catalog(c.ctx, 5, "")
		if err != nil || len(page.Repositories) != 5 || page.Repositories[0] != "bulk/repo0000" {
			t.Fatalf("unfiltered page = %v, %v", page.Repositories, err)
		}
		if c.fake.calls > 1 {
			t.Fatalf("unfiltered request queried the store %d times", c.fake.calls)
		}
	}
}

func TestCatalogNeverShowsRepositoriesOutsideTheGrant(t *testing.T) {
	t.Parallel()
	service, _ := catalogServiceWith(t, manyRepositories())
	none := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{
		ProjectGrants: []domainauth.ProjectGrant{{Project: "nothere", Role: domainauth.RepoRoleAdmin}},
		Scopes:        catalogScope(),
	})
	page, err := service.Catalog(none, 10, "")
	if err != nil || page.Repositories == nil || len(page.Repositories) != 0 {
		t.Fatalf("Catalog() = %#v, %v, want an empty non-nil page", page, err)
	}
}

// TestCatalogUniversalPrincipalsFetchExactlyLimit: a global admin or
// registry-wide reader with the catalog scope sees everything, so the store is
// asked for exactly limit rows (as before) and the page is identical to the
// filtered path; a scope-limited admin still goes through filtering.
func TestCatalogUniversalPrincipalsFetchExactlyLimit(t *testing.T) {
	t.Parallel()
	names := manyRepositories()
	service, fake := catalogServiceWith(t, names)
	var asked []int
	fake.onFetch = func(limit int) { asked = append(asked, limit) }

	for _, p := range []domainauth.Principal{
		{IsAdmin: true, Scopes: catalogScope()},
		{IsReadOnly: true, Scopes: catalogScope()},
	} {
		asked = nil
		ctx := ports.ContextWithPrincipal(context.Background(), p)
		page, err := service.Catalog(ctx, 7, "bulk/repo0010")
		if err != nil {
			t.Fatal(err)
		}
		if len(asked) != 1 || asked[0] != 7 {
			t.Fatalf("store asked for %v, want exactly [7]", asked)
		}
		if strings.Join(page.Repositories, ",") != strings.Join(names[11:18], ",") {
			t.Fatalf("page = %v", page.Repositories)
		}
		summaries, err := service.RepositorySummaries(ctx, 7, "bulk/repo0010")
		if err != nil || len(summaries) != 7 || summaries[0].Name != names[11] {
			t.Fatalf("summaries = %#v, %v", summaries, err)
		}
	}

	limited := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{IsAdmin: true, Scopes: []domainauth.Scope{{Type: "repository", Name: "zzz/last", Actions: []string{"pull"}, Canonical: "repository:zzz/last:pull"}}})
	page, err := service.Catalog(limited, 3, "")
	if err != nil || strings.Join(page.Repositories, ",") != "zzz/last" {
		t.Fatalf("scope-limited admin page = %v, %v, want only zzz/last", page.Repositories, err)
	}
}
