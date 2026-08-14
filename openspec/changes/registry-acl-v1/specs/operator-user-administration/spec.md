# Delta for operator-user-administration

## ADDED Requirements

### Requirement: User Records Carry The Registry-Wide Read-Only Role

Create-user and update-user inputs and outputs MUST support a registry-wide
read-only role flag, independent of `IsAdmin`, defaulting to `false`.

#### Scenario: Creating a user with the read-only flag persists it
- GIVEN an authenticated admin actor creates a user with the read-only role set
- WHEN the user is later listed
- THEN the listing reflects the read-only role

#### Scenario: Updating the read-only flag takes effect
- GIVEN an existing user without the read-only role
- WHEN an admin actor sets the read-only role on that user
- THEN subsequent authorization checks for that user honor the new role

### Requirement: Robot Identities Excluded From Default Human User Listing

The default user-listing endpoint MUST exclude robot accounts.

#### Scenario: Default listing omits robots
- GIVEN a robot account and a human user both exist
- WHEN the default user listing is requested
- THEN the robot account MUST NOT appear
