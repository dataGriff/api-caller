Feature: Todos
  The requests live in todos.http and auth.http; the phrases used here are
  declared on them with "# @step". Run me with: apic test

  Background:
    Given I am logged in

  Scenario: The list always has something in it
    When I list the todos
    Then the response status is 200
    And the response body "$.#" is not "0"

  Scenario: Create, update and delete a todo
    When a todo titled "Ship apic" is created
    Then the response status is 201
    And the response body "$.title" is "Ship apic"
    When I fetch the todo
    Then the response body "$.done" is "false"
    When I mark the todo done
    Then the response body "$.done" is "true"
    When I delete the todo
    Then the response status is 204
    When I run "get-deleted"
    Then the response status is 404

  Scenario: Redirects are followed unless a request opts out
    When I run "redirect-followed"
    Then the response status is 200
    When I run "redirect-raw"
    Then the response status is 302
    And the response header "location" contains "status/200"

  Scenario: The list can be filtered and paginated
    When I list the open todos
    Then the response status is 200
    And the response header "x-total-count" exists
    And the response body "$[0].done" is "false"

  Scenario: A todo needs a title
    When I create a todo with no title
    Then the response status is 422
    And the response body "$.fields.title" is "must not be empty"

  Scenario: A job finishes after a couple of polls
    When a job is submitted
    Then the response status is 202
    And the response body "$.state" is "queued"
    When I check the job
    Then the response body "$.state" is "running"
    When I wait for the job
    Then the response body "$.state" is "done"

  Scenario: Some routes take an API key instead of a token
    When I use the API key
    Then the response status is 200
    And the response body "$.ok" is "true"

  Scenario: GraphQL is a JSON POST like any other
    When I ask GraphQL for the done todos
    Then the response status is 200
    And the response body "$.data.todos[0].done" is "true"
