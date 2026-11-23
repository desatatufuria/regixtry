package tui

import (
	"context"
	"math/rand"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	appregixtry "regixtry/internal/app/regixtry"
	"regixtry/internal/ports"
)

// writeRecorder wraps the admin client and the registry service of a Model and
// records every call that changes data: the fuzz asserts that no mouse event
// ever causes one.
type writeRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *writeRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *writeRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

type recordingClient struct {
	AdminClient
	rec *writeRecorder
}

func (c recordingClient) ExecuteFeatureAction(ctx context.Context, s AdminSession, name, action string) (ports.FeatureActionResult, error) {
	c.rec.record("ExecuteFeatureAction")
	return c.AdminClient.ExecuteFeatureAction(ctx, s, name, action)
}

func (c recordingClient) InstallFeatureRuntime(ctx context.Context, s AdminSession, name, version string) (ports.FeatureRuntimeState, error) {
	c.rec.record("InstallFeatureRuntime")
	return c.AdminClient.InstallFeatureRuntime(ctx, s, name, version)
}

func (c recordingClient) UpgradeFeatureRuntime(ctx context.Context, s AdminSession, name, version string) (ports.FeatureRuntimeState, error) {
	c.rec.record("UpgradeFeatureRuntime")
	return c.AdminClient.UpgradeFeatureRuntime(ctx, s, name, version)
}

func (c recordingClient) RollbackFeatureRuntime(ctx context.Context, s AdminSession, name string) (ports.FeatureRuntimeState, error) {
	c.rec.record("RollbackFeatureRuntime")
	return c.AdminClient.RollbackFeatureRuntime(ctx, s, name)
}

func (c recordingClient) ConfigureFeature(ctx context.Context, s AdminSession, name string, in ports.FeatureConfigureInput) (ports.FeatureDetails, error) {
	c.rec.record("ConfigureFeature")
	return c.AdminClient.ConfigureFeature(ctx, s, name, in)
}

func (c recordingClient) UpdateScanPolicy(ctx context.Context, s AdminSession, in ports.ScanPolicySettings) (ports.ScanPolicySettings, error) {
	c.rec.record("UpdateScanPolicy")
	return c.AdminClient.UpdateScanPolicy(ctx, s, in)
}

func (c recordingClient) SetUpdateChannel(ctx context.Context, s AdminSession, channel string) (string, error) {
	c.rec.record("SetUpdateChannel")
	return c.AdminClient.SetUpdateChannel(ctx, s, channel)
}

func (c recordingClient) UpdateSigningPolicy(ctx context.Context, s AdminSession, in ports.SigningPolicySettings) (ports.SigningPolicySettings, error) {
	c.rec.record("UpdateSigningPolicy")
	return c.AdminClient.UpdateSigningPolicy(ctx, s, in)
}

func (c recordingClient) EnableFeature(ctx context.Context, s AdminSession, name string) (ports.FeatureDetails, error) {
	c.rec.record("EnableFeature")
	return c.AdminClient.EnableFeature(ctx, s, name)
}

func (c recordingClient) DisableFeature(ctx context.Context, s AdminSession, name string) (ports.FeatureDetails, error) {
	c.rec.record("DisableFeature")
	return c.AdminClient.DisableFeature(ctx, s, name)
}

func (c recordingClient) CreateUser(ctx context.Context, s AdminSession, in ports.AdminCreateUserInput) (ports.AdminUser, error) {
	c.rec.record("CreateUser")
	return c.AdminClient.CreateUser(ctx, s, in)
}

func (c recordingClient) ResetPassword(ctx context.Context, s AdminSession, in ports.AdminResetPasswordInput) error {
	c.rec.record("ResetPassword")
	return c.AdminClient.ResetPassword(ctx, s, in)
}

func (c recordingClient) PutUserGrant(ctx context.Context, s AdminSession, in ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error) {
	c.rec.record("PutUserGrant")
	return c.AdminClient.PutUserGrant(ctx, s, in)
}

func (c recordingClient) PutRepositoryGrant(ctx context.Context, s AdminSession, in ports.AdminPutRepositoryGrantInput) (ports.AdminRepositoryGrant, error) {
	c.rec.record("PutRepositoryGrant")
	return c.AdminClient.PutRepositoryGrant(ctx, s, in)
}

func (c recordingClient) DeleteRepositoryGrant(ctx context.Context, s AdminSession, repository, username string) error {
	c.rec.record("DeleteRepositoryGrant")
	return c.AdminClient.DeleteRepositoryGrant(ctx, s, repository, username)
}

func (c recordingClient) CreateUserAdminToken(ctx context.Context, s AdminSession, in ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	c.rec.record("CreateUserAdminToken")
	return c.AdminClient.CreateUserAdminToken(ctx, s, in)
}

func (c recordingClient) RevokeUserAdminToken(ctx context.Context, s AdminSession, userID, accessor string) error {
	c.rec.record("RevokeUserAdminToken")
	return c.AdminClient.RevokeUserAdminToken(ctx, s, userID, accessor)
}

func (c recordingClient) EnableUser(ctx context.Context, s AdminSession, id string) (ports.AdminUser, error) {
	c.rec.record("EnableUser")
	return c.AdminClient.EnableUser(ctx, s, id)
}

func (c recordingClient) DisableUser(ctx context.Context, s AdminSession, id string) (ports.AdminUser, error) {
	c.rec.record("DisableUser")
	return c.AdminClient.DisableUser(ctx, s, id)
}

func (c recordingClient) CreateRobot(ctx context.Context, s AdminSession, in ports.AdminCreateRobotInput) (ports.AdminCreatedRobot, error) {
	c.rec.record("CreateRobot")
	return c.AdminClient.CreateRobot(ctx, s, in)
}

func (c recordingClient) SetRepositoryOverride(ctx context.Context, s AdminSession, repository, feature string, in ports.RepositoryOverrideDetails) (ports.RepositoryOverrideDetails, error) {
	c.rec.record("SetRepositoryOverride")
	return c.AdminClient.SetRepositoryOverride(ctx, s, repository, feature, in)
}

func (c recordingClient) ClearRepositoryOverride(ctx context.Context, s AdminSession, repository, feature string) error {
	c.rec.record("ClearRepositoryOverride")
	return c.AdminClient.ClearRepositoryOverride(ctx, s, repository, feature)
}

func (c recordingClient) DeleteRobot(ctx context.Context, s AdminSession, id string) error {
	c.rec.record("DeleteRobot")
	return c.AdminClient.DeleteRobot(ctx, s, id)
}

type recordingService struct {
	QueryService
	rec *writeRecorder
}

func (s recordingService) DeleteManifest(ctx context.Context, repository, reference string) (appregixtry.DeletionDetails, error) {
	s.rec.record("DeleteManifest")
	return s.QueryService.DeleteManifest(ctx, repository, reference)
}

func (s recordingService) DeleteRepository(ctx context.Context, repository string) (appregixtry.RepositoryDeletionDetails, error) {
	s.rec.record("DeleteRepository")
	return s.QueryService.DeleteRepository(ctx, repository)
}

// drain runs cmd and every command the messages it produces return, feeding
// them back to the Model like the program loop; a quit request is dropped.
func drain(m Model, cmd tea.Cmd, depth int) Model {
	if cmd == nil || depth > 8 {
		return m
	}
	msg := cmd()
	if _, quit := msg.(tea.QuitMsg); quit {
		return m
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			m = drain(m, sub, depth+1)
		}
		return m
	}
	updated, next := m.Update(msg)
	return drain(updated.(Model), next, depth+1)
}

var fuzzKeys = []keyMsg{
	testKeyDown(), testKeyUp(), testKeyEnter(), testKeyEsc(), testKeyTab(), testKeyPgDown(), testKeyEnd(), testKeyHome(),
	testKeyRunes("/"), testKeyRunes("n"), testKeyRunes("x"), testKeyRunes("d"), testKeyRunes("e"), testKeyRunes("y"),
	testKeyRunes("r"), testKeyRunes("a"), testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("g"), testKeyRunes("o"),
	testKeyRunes("c"), testKeyRunes("p"), testKeyRunes("s"), testKeyRunes("t"), testKeyRunes("l"), testKeyF2(),
}

// fuzzTargets are the screens the fuzz starts on: every list screen of the
// shared mouse cases, plus the Overview, the configuration screens and the
// Update channel.
func fuzzTargets() []mouseCase {
	targets := append([]mouseCase(nil), mouseCases...)
	for _, to := range []screen{screenAdminOverview, screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenAdminUpdateChannel, screenAdminUsers} {
		targets = append(targets, mouseCase{name: string(to), build: func(t *testing.T, w, h int) Model {
			m := overviewModelAt(t, overviewRichClient(), w, h)
			updated, cmd := m.Update(navigateMsg{To: to})
			return runCmd(t, updated.(Model), cmd)
		}})
	}
	// The folder tree under a project: clicks toggle folders, select and open,
	// and none of them may ever delete.
	targets = append(targets, mouseCase{name: "console folder tree", build: func(t *testing.T, w, h int) Model {
		return consoleMouseModel(t, screenRepositories, newConsoleRepositoriesScreen(consoleRepos(treeNames...)).narrowedTo("odabank"), w, h)
	}})
	return targets
}

// TestMouseEventsNeverPanicNorChangeData throws a seeded mix of keys and mouse
// events (clicks anywhere, wheel, release, motion, other buttons, F2) at every
// screen. Keys may do whatever keys do; but no step that is a mouse event may
// call a write on the admin client or the registry service, nor open a
// browser.
func TestMouseEventsNeverPanicNorChangeData(t *testing.T) {
	opened := 0
	previous := openURLInBrowser
	openURLInBrowser = func(string) error { opened++; return nil }
	t.Cleanup(func() { openURLInBrowser = previous })

	keyWrites := 0
	t.Cleanup(func() {
		if keyWrites == 0 && !t.Failed() {
			t.Error("no key ever caused a write: the recorder is not wired to the screens")
		}
	})
	for _, c := range fuzzTargets() {
		t.Run(c.name, func(t *testing.T) {
			for seed := int64(0); seed < 10; seed++ {
				rng := rand.New(rand.NewSource(seed))
				size := geometrySizes[rng.Intn(len(geometrySizes))]
				m := c.build(t, size.w, size.h)
				rec := &writeRecorder{}
				m.adminClient = recordingClient{AdminClient: m.adminClient, rec: rec}
				if m.service != nil {
					m.service = recordingService{QueryService: m.service, rec: rec}
				}
				for step := range 100 {
					var msg tea.Msg
					mouse := true
					x, y := rng.Intn(size.w+6)-2, rng.Intn(size.h+6)-2
					switch rng.Intn(10) {
					case 0, 1, 2, 3:
						msg = mouseClickAt(x, y)
					case 4, 5:
						msg = mouseWheel([]tea.MouseButton{tea.MouseWheelUp, tea.MouseWheelDown, tea.MouseWheelLeft}[rng.Intn(3)])
					case 6:
						msg = tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
					case 7:
						msg = tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
					case 8:
						msg = tea.MouseClickMsg{X: x, Y: y, Button: []tea.MouseButton{tea.MouseRight, tea.MouseMiddle}[rng.Intn(2)]}
					default:
						msg, mouse = fuzzKeys[rng.Intn(len(fuzzKeys))], false
					}
					writes, urls := rec.count(), opened
					updated, cmd := m.Update(msg)
					m = drain(updated.(Model), cmd, 0)
					if step%12 == 0 {
						_ = m.View() // rendering must not panic either
					}
					if !mouse {
						keyWrites += rec.count() - writes
					}
					if mouse && (rec.count() != writes || opened != urls) {
						t.Fatalf("seed %d step %d: %T at (%d,%d) wrote %v / opened %d URLs", seed, step, msg, x, y, rec.calls[writes:], opened-urls)
					}
				}
			}
		})
	}
}
