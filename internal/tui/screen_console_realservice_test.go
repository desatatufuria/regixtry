package tui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	appregixtry "regixtry/internal/app/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

type realOperator struct{}

func (realOperator) Authorize(context.Context, ports.Action) error { return nil }
func (realOperator) Challenge(ports.Action) ports.Challenge        { return ports.Challenge{} }

type realSvc struct {
	*appregixtry.Service
	mu       sync.Mutex
	calls    []string
	failRepo map[string]bool
	noDelete bool
}

func (z *realSvc) DeleteManifest(ctx context.Context, r, ref string) (appregixtry.DeletionDetails, error) {
	z.mu.Lock()
	z.calls = append(z.calls, "tag "+r+":"+ref)
	nd := z.noDelete
	z.mu.Unlock()
	if nd {
		return appregixtry.DeletionDetails{}, nil
	}
	return z.Service.DeleteManifest(ctx, r, ref)
}
func (z *realSvc) DeleteRepository(ctx context.Context, r string) (appregixtry.RepositoryDeletionDetails, error) {
	z.mu.Lock()
	z.calls = append(z.calls, "repo "+r)
	nd, f := z.noDelete, z.failRepo[r]
	z.mu.Unlock()
	if f {
		return appregixtry.RepositoryDeletionDetails{}, fmt.Errorf("boom %s", r)
	}
	if nd {
		return appregixtry.RepositoryDeletionDetails{}, nil
	}
	return z.Service.DeleteRepository(ctx, r)
}

func realNew(t *testing.T) (*realSvc, string) {
	dir := t.TempDir()
	blob, err := fsblob.New(filepath.Join(dir, "c"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := metadata.New(filepath.Join(dir, "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { meta.Close() })
	s := appregixtry.NewService(blob, meta, realOperator{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	s.SetDeleteEnabled(true)
	return &realSvc{Service: s, failRepo: map[string]bool{}}, dir
}

func realPush(t *testing.T, s *realSvc, repo, tag, content string) string {
	ctx := context.Background()
	up, err := s.BeginUpload(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendUpload(ctx, repo, up.ID, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	d := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
	if _, err := s.CompleteUpload(ctx, repo, up.ID, d, nil); err != nil {
		t.Fatal(err)
	}
	m := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + d + `","size":` + fmt.Sprint(len(content)) + `},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + d + `","size":` + fmt.Sprint(len(content)) + `}]}`)
	if _, err := s.PublishManifest(ctx, repo, tag, "application/vnd.oci.image.manifest.v1+json", m); err != nil {
		t.Fatal(err)
	}
	return d
}

func realSeed(t *testing.T) (*realSvc, string) {
	s, dir := realNew(t)
	for _, r := range []string{"team/api", "team/api2", "team/a", "team2/api", "team", "ops/x"} {
		realPush(t, s, r, "latest", "content-A")
		realPush(t, s, r, "v1", "content-A") // same manifest, 2 tags
		realPush(t, s, r, "v10", "content-B")
	}
	realPush(t, s, "team/api", "v1.1", "content-C")
	return s, dir
}

func realTags(t *testing.T, s *realSvc, repo string) []string {
	tags, err := s.Service.TagDetails(context.Background(), repo, 100, "")
	if err != nil {
		return []string{"ERR:" + err.Error()}
	}
	var out []string
	for _, x := range tags {
		out = append(out, x.Name)
	}
	slices.Sort(out)
	return out
}
func realRepos(t *testing.T, s *realSvc) []string {
	rs, _ := s.Service.RepositorySummaries(context.Background(), 100, "")
	var out []string
	for _, r := range rs {
		out = append(out, r.Name)
	}
	slices.Sort(out)
	return out
}
func realBlobCount(dir string) int {
	n := 0
	filepath.Walk(filepath.Join(dir, "c"), func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func realModel(t *testing.T, s *realSvc, opts ...Option) Model {
	m := NewModel(s, append([]Option{WithAdminClient(&fakeAdminClient{})}, opts...)...)
	m = runCmd(t, m, m.Init())
	return charResize(t, m, 160, 30)
}

func TestConsoleDeletesNamedTargetOnlyWithTheRealService(t *testing.T) {
	s, dir := realSeed(t)
	blobs0 := realBlobCount(dir)
	allRepos := realRepos(t, s)
	m := realModel(t, s)
	// cursor to team/api: repos sorted by name?
	// move to team/api
	for i := 0; i < 10 && !strings.Contains(consoleView(runKey(t, m, "enter")), "team/api › Tags"); i++ {
		m = runKey(t, m, "down")
	}
	m = runKey(t, m, "enter")
	// select tag v1
	for i := 0; i < 10; i++ {
		m = runKey(t, m, "d")
		if strings.Contains(consoleView(m), "team/api:v1 ") || strings.Contains(consoleView(m), "team/api:v1\n") {
			break
		}
		m = runKey(t, m, "esc")
		m = runKey(t, m, "down")
	}
	m = runKey(t, m, "y")
	if !slices.Equal(s.calls, []string{"tag team/api:v1"}) {
		t.Fatalf("calls %v", s.calls)
	}
	if got := realTags(t, s, "team/api"); !slices.Equal(got, []string{"latest", "v1.1", "v10"}) {
		t.Fatalf("tags %v", got)
	}
	for _, r := range []string{"team/api2", "team/a", "team2/api", "team", "ops/x"} {
		if got := realTags(t, s, r); !slices.Equal(got, []string{"latest", "v1", "v10"}) {
			t.Fatalf("%s tags %v", r, got)
		}
	}
	// manifest of v1's twin still resolves
	if _, err := s.ResolveManifest(context.Background(), "team/api", "latest"); err != nil {
		t.Fatal(err)
	}
	// repo delete team/api
	m = runKey(t, m, "esc")
	s.calls = nil
	for i := 0; i < 10; i++ {
		m = runKey(t, m, "d")
		if strings.Contains(consoleView(m), "Delete repository team/api\n") || strings.Contains(consoleView(m), "Delete repository team/api ") {
			break
		}
		m = runKey(t, m, "esc")
		m = runKey(t, m, "down")
	}
	m = runKey(t, m, "y")
	if !slices.Equal(s.calls, []string{"repo team/api"}) {
		t.Fatalf("calls %v", s.calls)
	}
	got := realRepos(t, s)
	want := slices.DeleteFunc(slices.Clone(allRepos), func(r string) bool { return r == "team/api" })
	if !slices.Equal(got, want) {
		t.Fatalf("repos %v want %v", got, want)
	}
	if realBlobCount(dir) != blobs0 {
		t.Fatalf("blob files changed %d -> %d", blobs0, realBlobCount(dir))
	}
	// project delete "team"
	m = runKey(t, m, "p")
	s.calls = nil
	for i := 0; i < 10; i++ {
		m = runKey(t, m, "d")
		if strings.Contains(consoleView(m), "Delete project team\n") || strings.Contains(consoleView(m), "Delete project team ") {
			break
		}
		m = runKey(t, m, "esc")
		m = runKey(t, m, "down")
	}
	m = runKey(t, m, "y")
	slices.Sort(s.calls)
	if !slices.Equal(s.calls, []string{"repo team/a", "repo team/api2"}) {
		t.Fatalf("calls %v", s.calls)
	}
	if got := realRepos(t, s); !slices.Equal(got, []string{"ops/x", "team", "team2/api"}) {
		t.Fatalf("repos %v", got)
	}
	if realBlobCount(dir) != blobs0 {
		t.Fatalf("blob files changed")
	}
}
