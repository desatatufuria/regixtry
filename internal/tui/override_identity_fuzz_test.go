package tui

import (
	"fmt"
	"math/rand"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Seeded fuzz: random keys and out-of-order results between "o" and Enter. Every
// save/clear/load result must carry the repository and feature of the editor it
// was opened for, which is the repository that was selected when "o" was pressed,
// and no save may be issued before the stored override was read.
func ovPeek(m Model, to screen) (overridesList, bool) {
	switch to {
	case screenSecurityTrivyRepos:
		s, ok := m.adminScreens[slotTrivyRepos].(trivyReposScreen)
		return s.overridesList, ok
	case screenSecurityGitleaksRepos:
		s, ok := m.adminScreens[slotGitleaksRepos].(featureOverridesScreen)
		return s.overridesList, ok
	}
	s, ok := m.adminScreens[slotSigningRepos].(featureOverridesScreen)
	return s.overridesList, ok
}

func ovFlatten(cmd tea.Cmd, out *[]tea.Cmd) {
	if cmd != nil {
		*out = append(*out, cmd)
	}
}

func TestOverridesEveryRequestTargetsTheEditorsOpenedRepositoryUnderRandomKeys(t *testing.T) {
	keys := []keyMsg{testKeyDown(), testKeyDown(), testKeyUp(), testKeyPgDown(), testKeyPgUp(), testKeyHome(), testKeyEnd(),
		testKeyTab(), testKeyShiftTab(), testKeyEnter(), testKeyEnter(), testKeyEsc(), testKeySpace(), testKeyRunes("o"), testKeyRunes("o"),
		testKeyRunes("r"), testKeyRunes("j"), testKeyRunes("k"), testKeyRunes("y"), testKeyRunes("n"), testKeyRunes("x"), testKeyRunes("q"), testKeyRunes("a"), testKeyBackspace()}
	for _, sc := range ovScreens {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			violations := 0
			saves := 0
			for seed := int64(0); seed < 25; seed++ {
				rng := rand.New(rand.NewSource(seed))
				client := ovClient()
				known := ovManyRepositories(200)
				m := ovModelAt(t, client, known, sc.to, 160, 30)
				targets := map[uint64][2]string{}
				var pending []tea.Cmd
				for step := 0; step < 100; step++ {
					l, ok := ovPeek(m, sc.to)
					if !ok {
						t.Fatal("slot lost")
					}
					if rng.Intn(100) < 35 && len(pending) > 0 {
						i := rng.Intn(len(pending))
						c := pending[i]
						pending = append(pending[:i], pending[i+1:]...)
						msg := c()
						var msgs []tea.Msg
						if b, ok := msg.(tea.BatchMsg); ok {
							for _, sub := range b {
								if sub != nil {
									pending = append(pending, sub)
								}
							}
							continue
						}
						msgs = append(msgs, msg)
						for _, msg := range msgs {
							switch x := msg.(type) {
							case overrideSavedMsg:
								saves++
								if tg, ok := targets[x.editor]; !ok || tg != [2]string{x.repository, x.feature} {
									violations++
									t.Errorf("seed %d: save/clear msg editor %d for %v targets %v", seed, x.editor, [2]string{x.repository, x.feature}, tg)
								}
							case overrideLoadedMsg:
								if tg, ok := targets[x.editor]; !ok || tg != [2]string{x.repository, x.feature} {
									t.Errorf("seed %d: load msg wrong target", seed)
								}
							}
							updated, next := m.Update(msg)
							m = updated.(Model)
							ovFlatten(next, &pending)
						}
						continue
					}
					k := keys[rng.Intn(len(keys))]
					if !l.editor.Active() {
						if isEscKey(k) || isEnterKey(k) || isTabKey(k) {
							continue
						}
					}
					row, hasRow := l.selectedRow()
					before := l.editor
					updated, next := m.Update(k)
					m = updated.(Model)
					l2, _ := ovPeek(m, sc.to)
					if l2.editor.Active() && (!before.Active() || before.id != l2.editor.id) {
						if !hasRow || l2.editor.repository != row.Repository || l2.editor.feature != sc.feature {
							t.Errorf("seed %d: editor opened for %q/%s but selected %q", seed, l2.editor.repository, l2.editor.feature, row.Repository)
						}
						targets[l2.editor.id] = [2]string{l2.editor.repository, l2.editor.feature}
					}
					if before.Active() && before.id == l2.editor.id && !before.busy && l2.editor.busy && !before.loaded {
						violations++
						t.Errorf("seed %d: save before the override was read", seed)
					}
					if before.Active() && before.id == l2.editor.id && l2.editor.repository != before.repository {
						t.Errorf("seed %d: editor retargeted", seed)
					}
					ovFlatten(next, &pending)
					_ = fmt.Sprint
				}
			}
			t.Logf("violations %d, save/clear results seen %d", violations, saves)
		})
	}
}
