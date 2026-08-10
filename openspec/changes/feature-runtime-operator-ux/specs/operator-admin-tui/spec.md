# Delta for operator-admin-tui

## ADDED Requirements

### Requirement: State-Aware Feature Cursor Actions

The feature screen MUST expose only actions valid for the selected state. Help text SHALL list keys, MUST update on selection or state changes, and MUST NOT advertise unavailable actions.

#### Scenario: Available actions match selected runtime state

- GIVEN the feature screen has a selected feature
- WHEN its state is refreshed or the cursor moves
- THEN the TUI SHALL recompute valid actions
- AND the help text SHALL show only those actions

#### Scenario: Unknown or unavailable state degrades safely

- GIVEN the selected feature lacks data needed for a mutation
- WHEN the operator views the feature help or presses an unavailable action key
- THEN the TUI MUST keep the action unavailable
- AND it MUST show guidance instead of executing the mutation

### Requirement: Thin Feature Mutation Flow

Feature install, upgrade, rollback, enable, and disable actions MUST stay in the feature screen and SHALL call existing backend feature operations. After each action, the TUI MUST refresh feature list and status, MUST surface progress or result messaging, and MUST NOT add screens, wizards, or job abstractions.

#### Scenario: Successful cursor action refreshes the feature view

- GIVEN an operator triggers a valid feature action
- WHEN the backend operation succeeds
- THEN the TUI SHALL show completion feedback in-screen
- AND it SHALL refresh feature list and selected-feature status

#### Scenario: Failed cursor action stays thin and truthful

- GIVEN an operator triggers a valid feature action
- WHEN the backend operation fails or is rejected
- THEN the TUI MUST show recoverable failure feedback in-screen
- AND it MUST NOT open a new orchestration flow or extra mutation screen
