package tui

import (
	"fmt"
	"math/rand"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func fuzzRobots(n int, duplicateNames bool) []ports.AdminRobot {
	out := make([]ports.AdminRobot, n)
	for i := range out {
		name := fmt.Sprintf("robot-%03d", i)
		if duplicateNames {
			name = fmt.Sprintf("robot-%03d", i%3)
		}
		out[i] = ports.AdminRobot{ID: fmt.Sprintf("id-%03d", i), Username: name, Repository: "team/api", Role: domainauth.RepoRoleWriter, Enabled: i%2 == 0}
	}
	return out
}

// TestDeleteTargetIsAlwaysTheRobotSelectedWhenXWasPressed throws a random
// sequence of moves, list reloads (shuffled or empty), results and keys
// between x and y: whatever happens, the robot deleted is the one that was
// selected when x was pressed, never the one under the cursor later.
func TestDeleteTargetIsAlwaysTheRobotSelectedWhenXWasPressed(t *testing.T) {
	t.Parallel()
	ran := 0
	for seed := int64(0); seed < 300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		sizes := []int{1, 2, 5, 200}
		client := newAccessClient()
		client.robots = fuzzRobots(sizes[rng.Intn(len(sizes))], seed%2 == 0)
		client.tokens = map[string][]ports.AdminToken{}
		env := accessEnv(client)
		s := openRobotsScreen(t, client, env)
		var cur adminScreen = s
		for range rng.Intn(260) {
			if rng.Intn(2) == 0 {
				cur, _, _ = cur.Update(env, testKeyDown())
			} else {
				cur, _, _ = cur.Update(env, testKeyUp())
			}
		}
		want, ok := cur.(robotsScreen).selectedRobot()
		if !ok {
			t.Fatalf("seed %d: no robot selected", seed)
		}
		cur, _, _ = cur.Update(env, testKeyRunes("x"))
		if !cur.(robotsScreen).confirm.Active() {
			t.Fatalf("seed %d: x opened no dialog", seed)
		}
		for range rng.Intn(30) {
			switch rng.Intn(7) {
			case 0:
				cur, _, _ = cur.Update(env, testKeyDown())
			case 1:
				cur, _, _ = cur.Update(env, testKeyUp())
			case 2:
				shuffled := append([]ports.AdminRobot(nil), client.robots...)
				rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
				cur, _, _ = cur.Update(env, robotsLoadedMsg{gen: s.gen, robots: shuffled})
			case 3:
				cur, _, _ = cur.Update(env, robotsLoadedMsg{gen: s.gen})
			case 4:
				cur, _, _ = cur.Update(env, robotEnabledMsg{gen: s.gen, robotID: want.ID, name: "z"})
			case 5:
				key := []string{"d", "e", "g", "t", "n", "r", "j", "k"}[rng.Intn(8)]
				cur, _, _ = cur.Update(env, testKeyRunes(key))
			case 6:
				cur, _, _ = cur.Update(env, robotsTokensLoadedMsg{gen: s.gen})
			}
			if !cur.(robotsScreen).confirm.Active() {
				break
			}
		}
		if !cur.(robotsScreen).confirm.Active() {
			continue // the chaos closed the dialog: nothing to confirm
		}
		_, cmd, _ := cur.Update(env, testKeyRunes("y"))
		if cmd == nil {
			t.Fatalf("seed %d: y sent nothing", seed)
		}
		cmd()
		ran++
		if client.deleteRobotCalls != 1 || client.lastDeleteRobotUserID != want.ID {
			t.Fatalf("seed %d: deleted %q in %d calls, want %q once", seed, client.lastDeleteRobotUserID, client.deleteRobotCalls, want.ID)
		}
	}
	if ran == 0 {
		t.Fatal("no run reached the confirm")
	}
}
