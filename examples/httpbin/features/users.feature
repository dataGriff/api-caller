Feature: Users
  The requests live in users.http; the phrases used here are declared on
  them with "# @step".

  Scenario: Fetch a user by id
    When I fetch user 42
    Then the response status is 200
    And the response body "$.args.id" is "42"
    And the response header "content-type" contains "json"
    And the response body contains:
      """
      {"args": {"id": "42", "expand": "profile"}}
      """

  Scenario: Create a user and reuse the id it was given
    When a user named "carol" is created
    Then the response is successful
    And the response body "$.json.email" ends with "@example.com"
    When I capture the response body "$.json.name" as "createdName"
    And I run "get-user" with:
      | userId | {{createdName}} |
    Then the response body "$.args.id" is "carol"

  Scenario Outline: Any id echoes back
    When I fetch user <id>
    Then the response body "$.args.id" is "<id>"
    Examples:
      | id  |
      | 1   |
      | 999 |

  @slow
  Scenario: Missing endpoints are client errors
    When I run "not-found"
    Then the response status is 404
