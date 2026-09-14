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
