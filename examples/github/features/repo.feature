Feature: GitHub repo
  The requests live in repo.http; the phrases used here are declared on
  them with "# @step". Needs a real token in http-client.private.env.json
  (see README.md at the repo root for how to get one).

  Scenario: The token authenticates me
    When I ask who I am on GitHub
    Then the response status is 200
    And the response body "$.login" exists

  Scenario: Fetch a public repo
    When I fetch the repo
    Then the response is successful
    And the response body "$.full_name" is "octocat/Hello-World"

  Scenario: List open issues
    When I list open issues on the repo
    Then the response status is 200
